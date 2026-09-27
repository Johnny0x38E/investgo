package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"investgo/internal/core"
	"investgo/internal/core/instrument"
	"investgo/internal/logger"
)

const quoteUpsertTimeout = 8 * time.Second

// UpsertItem saves or updates a tracked item and fetches a fresh quote when a live provider is available.
// ctx bounds the quote fetch. A nil ctx is treated as context.Background().
func (s *Store) UpsertItem(ctx context.Context, input core.WatchlistItem) (core.StateSnapshot, error) {
	item, err := sanitiseItem(input)
	if err != nil {
		return core.StateSnapshot{}, err
	}

	// First extract runtime dependencies and old values within read lock to avoid holding write lock during subsequent network requests.
	s.mu.RLock()
	provider := s.activeQuoteProviderLocked(item.Market)
	var existing *core.WatchlistItem
	if input.ID != "" {
		if index := s.findItemIndexLocked(input.ID); index >= 0 {
			copy := s.state.Items[index]
			existing = &copy
		}
	} else {
		for _, it := range s.state.Items {
			if it.Symbol == item.Symbol && it.Market == item.Market {
				s.mu.RUnlock()
				return core.StateSnapshot{}, fmt.Errorf("Item already exists in the list: %s (%s)", item.Symbol, item.Market)
			}
		}
	}
	s.mu.RUnlock()

	requestedName := strings.TrimSpace(item.Name)
	if existing != nil {
		item = inheritLiveFields(item, *existing)
		if existing.PinnedAt != nil {
			item.PinnedAt = ptrTime(*existing.PinnedAt)
		} else {
			item.PinnedAt = nil
		}
		if existing.HasCustomName && !item.HasCustomName && requestedName != "" && requestedName != strings.TrimSpace(existing.DefaultName) {
			item.HasCustomName = true
		}
	}

	if provider != nil {
		// Fetch one quote immediately after saving the item to ensure current price always comes from a unified quote source.
		// The timeout is derived from the request context so a canceled client stops the upstream call.
		if ctx == nil {
			ctx = context.Background()
		}
		fetchCtx, cancel := context.WithTimeout(ctx, quoteUpsertTimeout)
		quotes, quoteErr := provider.Fetch(fetchCtx, []core.WatchlistItem{item})
		cancel()

		if quoteErr == nil {
			if target, resolveErr := core.ResolveQuoteTarget(item); resolveErr == nil {
				if quote, ok := quotes[target.Key]; ok {
					applyQuoteToItem(&item, quote)
				}
			}
		}
	}

	resolveSavedDisplay(&item, existing, requestedName)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-check under the write lock. The scan above runs before the quote
	// fetch, so two concurrent creates of the same symbol and market can both
	// pass it. instruments are unique on (asset_class, market, exchange, symbol)
	// and watchlist_entries.instrument_id is UNIQUE, but this request appends
	// to the in-memory list before save, so the lock is what stops the duplicate.
	if s.duplicateItemLocked(item.Symbol, item.Market, item.ID) {
		return core.StateSnapshot{}, fmt.Errorf("Item already exists in the list: %s (%s)", item.Symbol, item.Market)
	}

	if item.ID == "" {
		item.ID = newID("item")
		item.UpdatedAt = time.Now()
		s.state.Items = append(s.state.Items, item)
		s.logInfo("watchlist", fmt.Sprintf("added item %s", item.Symbol))
	} else {
		index := s.findItemIndexLocked(item.ID)
		if index == -1 {
			return core.StateSnapshot{}, fmt.Errorf("Item not found: %s", item.ID)
		}
		item.UpdatedAt = time.Now()
		s.state.Items[index] = item
		s.logInfo("watchlist", fmt.Sprintf("updated item %s", item.Symbol))
	}

	s.runtime.QuoteSource = s.quoteProviderSummaryLocked()
	now := time.Now()
	s.state.UpdatedAt = now
	s.holdingsUpdatedAt = now
	s.evaluateAlertsLocked()
	s.invalidateAllCachesLocked()
	if err := s.saveLocked(); err != nil {
		s.logError("storage", fmt.Sprintf("save state failed after item update: %v", err))
		return core.StateSnapshot{}, err
	}

	return s.snapshotLocked(), nil
}

// ApplyInstrumentDisplay copies an instrument display alias onto matching
// watchlist/holding rows so a rename made in the pool manager is visible
// everywhere without waiting for the next quote refresh.
func (s *Store) ApplyInstrumentDisplay(value instrument.Instrument) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	display := value.Display()
	official := value.Name
	custom := value.HasCustomName()
	for index := range s.state.Items {
		identity, ok := watchlistInstrumentIdentity(s.state.Items[index])
		if !ok || identity != value.Identity() {
			continue
		}
		item := s.state.Items[index]
		if item.Name == display && item.DefaultName == official && item.HasCustomName == custom {
			continue
		}
		item.Name = display
		item.DefaultName = official
		item.HasCustomName = custom
		item.UpdatedAt = time.Now()
		s.state.Items[index] = item
		changed = true
	}
	if !changed {
		return nil
	}

	now := time.Now()
	s.state.UpdatedAt = now
	s.holdingsUpdatedAt = now
	s.invalidateAllCachesLocked()
	if err := s.saveLocked(); err != nil {
		s.logError("storage", fmt.Sprintf("save state failed after display name update: %v", err))
		return err
	}
	return nil
}

func watchlistInstrumentIdentity(item core.WatchlistItem) (instrument.Identity, bool) {
	normalized, err := instrument.Normalize(instrument.Instrument{
		AssetClass:    instrument.AssetClass(sqliteAssetClass(item.Market)),
		Symbol:        item.Symbol,
		Name:          firstNonEmptyDisplay(item.DefaultName, item.Name, item.Symbol),
		Market:        item.Market,
		QuoteCurrency: item.Currency,
	})
	if err != nil {
		return instrument.Identity{}, false
	}
	return normalized.Identity(), true
}

func firstNonEmptyDisplay(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// SetItemPinned pins or unpins the specified item; pinned items sort to the top of all list views.
func (s *Store) SetItemPinned(id string, pinned bool) (core.StateSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.findItemIndexLocked(id)
	if index == -1 {
		return core.StateSnapshot{}, fmt.Errorf("Item not found: %s", id)
	}

	now := time.Now()
	item := s.state.Items[index]
	if pinned {
		item.PinnedAt = &now
	} else {
		item.PinnedAt = nil
	}
	s.state.Items[index] = item
	s.state.UpdatedAt = now
	s.invalidateAllCachesLocked()
	// Pin toggles are pure UI preference and acceptable to lose on a hard crash;
	// debounce the write so rapid pin/unpin bursts collapse into one disk write
	// instead of N synchronous marshals under the write lock.
	s.markDirtyLocked()

	action := "unpinned"
	if pinned {
		action = "pinned"
	}
	s.logInfo("watchlist", fmt.Sprintf("%s item %s", action, item.Symbol))

	return s.snapshotLocked(), nil
}

// DeleteItem deletes the specified item and synchronously deletes its associated alert rules.
func (s *Store) DeleteItem(id string) (core.StateSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.findItemIndexLocked(id)
	if index == -1 {
		return core.StateSnapshot{}, fmt.Errorf("Item not found: %s", id)
	}

	itemSymbol := s.state.Items[index].Symbol
	s.state.Items = append(s.state.Items[:index], s.state.Items[index+1:]...)
	// After deleting the item, alerts attached to it must also be cleared to avoid dangling references.
	filteredAlerts := s.state.Alerts[:0]
	for _, alert := range s.state.Alerts {
		if alert.ItemID != id {
			filteredAlerts = append(filteredAlerts, alert)
		}
	}
	s.state.Alerts = filteredAlerts
	now := time.Now()
	s.state.UpdatedAt = now
	s.holdingsUpdatedAt = now
	s.invalidateAllCachesLocked()

	if err := s.saveLocked(); err != nil {
		s.logError("storage", fmt.Sprintf("save state failed after item delete: %v", err))
		return core.StateSnapshot{}, err
	}

	s.logInfo("watchlist", fmt.Sprintf("deleted item %s", itemSymbol))

	return s.snapshotLocked(), nil
}

// UpsertAlert adds or updates a price alert rule.
func (s *Store) UpsertAlert(input core.AlertRule) (core.StateSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	alert, err := sanitiseAlert(input)
	if err != nil {
		return core.StateSnapshot{}, err
	}
	if s.findItemIndexLocked(alert.ItemID) == -1 {
		return core.StateSnapshot{}, fmt.Errorf("Alert item not found: %s", alert.ItemID)
	}

	if alert.ID == "" {
		alert.ID = newID("alert")
		alert.UpdatedAt = time.Now()
		s.state.Alerts = append(s.state.Alerts, alert)
		s.logInfo("alerts", fmt.Sprintf("created alert %s", alert.Name))
	} else {
		index := s.findAlertIndexLocked(alert.ID)
		if index == -1 {
			return core.StateSnapshot{}, fmt.Errorf("Alert not found: %s", alert.ID)
		}
		alert.UpdatedAt = time.Now()
		s.state.Alerts[index] = alert
		s.logInfo("alerts", fmt.Sprintf("updated alert %s", alert.Name))
	}

	s.state.UpdatedAt = time.Now()
	s.evaluateAlertsLocked()
	s.invalidateAllCachesLocked()
	if err := s.saveLocked(); err != nil {
		s.logError("storage", fmt.Sprintf("save state failed after alert update: %v", err))
		return core.StateSnapshot{}, err
	}

	return s.snapshotLocked(), nil
}

// DeleteAlert deletes the specified alert rule.
func (s *Store) DeleteAlert(id string) (core.StateSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.findAlertIndexLocked(id)
	if index == -1 {
		return core.StateSnapshot{}, fmt.Errorf("Alert not found: %s", id)
	}

	alertName := s.state.Alerts[index].Name
	s.state.Alerts = append(s.state.Alerts[:index], s.state.Alerts[index+1:]...)
	s.state.UpdatedAt = time.Now()
	s.invalidateAllCachesLocked()

	if err := s.saveLocked(); err != nil {
		s.logError("storage", fmt.Sprintf("save state failed after alert delete: %v", err))
		return core.StateSnapshot{}, err
	}

	s.logInfo("alerts", fmt.Sprintf("deleted alert %s", alertName))

	return s.snapshotLocked(), nil
}

func (s *Store) duplicateItemLocked(symbol, market, exceptID string) bool {
	for _, it := range s.state.Items {
		if exceptID != "" && it.ID == exceptID {
			continue
		}
		if it.Symbol == symbol && it.Market == market {
			return true
		}
	}
	return false
}

// UpdateSettings applies a partial settings update and immediately persists it.
// Omitted fields keep their stored values. See SettingsUpdate.
func (s *Store) UpdateSettings(input SettingsUpdate) (core.StateSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings, err := sanitiseSettings(input, s.state.Settings, s.quoteProviders, s.quoteSourceOptions)
	if err != nil {
		return core.StateSnapshot{}, err
	}

	s.state.Settings = settings
	now := time.Now()
	s.state.UpdatedAt = now
	s.holdingsUpdatedAt = now
	s.invalidateAllCachesLocked()
	if err := s.saveLocked(); err != nil {
		s.logError("storage", fmt.Sprintf("save state failed after settings update: %v", err))
		return core.StateSnapshot{}, err
	}

	s.logInfo(
		"settings",
		fmt.Sprintf(
			"updated settings: cn=%s hk=%s us=%s cacheTTL=%ds theme=%s color=%s developerMode=%t",
			settings.CNQuoteSource,
			settings.HKQuoteSource,
			settings.USQuoteSource,
			settings.HotCacheTTLSeconds,
			settings.ThemeMode,
			settings.ColorTheme,
			settings.DeveloperMode,
		),
	)

	return s.snapshotLocked(), nil
}

// sanitiseItem normalizes item information and performs basic validation.
func sanitiseItem(input core.WatchlistItem) (core.WatchlistItem, error) {
	item := input
	item.Name = strings.TrimSpace(item.Name)
	item.Thesis = strings.TrimSpace(item.Thesis)
	item.Tags = normalizeTags(item.Tags)

	target, err := core.ResolveQuoteTarget(item)
	if err != nil {
		return core.WatchlistItem{}, err
	}

	item.Symbol = target.DisplaySymbol
	item.Market = target.Market
	item.Currency = target.Currency
	item.QuoteSource = strings.TrimSpace(item.QuoteSource)

	// When DCA entries are present, normalize them and re-derive Quantity and CostPrice:
	//   1. If a buy price is recorded (Price > 0), use Price × Shares as the effective cost.
	//   2. Otherwise, derive effective cost from the invested amount net of fees: max(Amount − Fee, 0).
	//   Weighted average cost price = Σ effectiveCost_i / Σ Shares_i
	if len(item.DCAEntries) > 0 {
		validEntries, totalShares, averageCost := normalizeDCAEntries(item.DCAEntries, newID)
		item.DCAEntries = validEntries
		if len(item.DCAEntries) > 0 {
			item.Quantity = totalShares
			if totalShares > 0 {
				item.CostPrice = averageCost
			}
		}
	}

	if item.Quantity < 0 {
		return core.WatchlistItem{}, errors.New("Quantity must not be negative")
	}
	if item.CostPrice < 0 || item.CurrentPrice < 0 {
		return core.WatchlistItem{}, errors.New("Price must not be negative")
	}

	if item.AcquiredAt != nil {
		normalized := time.Date(item.AcquiredAt.Year(), item.AcquiredAt.Month(), item.AcquiredAt.Day(), 0, 0, 0, 0, time.UTC)
		item.AcquiredAt = &normalized
	}

	// Watch-only items (no shares held, no DCA records) should never carry an
	// acquisition date — clear it so the overview trend correctly excludes them.
	if item.Quantity == 0 && len(item.DCAEntries) == 0 {
		item.AcquiredAt = nil
	}

	return item, nil
}

// sanitiseAlert normalizes alert rules and performs basic validation.
func sanitiseAlert(input core.AlertRule) (core.AlertRule, error) {
	return sanitiseAlertRule(input)
}

// logInfo writes info level logs when logbook is available.
func (s *Store) logInfo(scope, message string) {
	if s.logs != nil {
		s.logs.Info("backend", scope, redactSensitiveLogText(message))
	}
}

// logWarn writes warn level logs when logbook is available.
func (s *Store) logWarn(scope, message string) {
	if s.logs != nil {
		s.logs.Warn("backend", scope, redactSensitiveLogText(message))
	}
}

// logError writes error level logs when logbook is available.
func (s *Store) logError(scope, message string) {
	if s.logs != nil {
		s.logs.Error("backend", scope, redactSensitiveLogText(message))
	}
}

func redactSensitiveLogText(message string) string {
	return logger.RedactSensitiveText(message)
}
