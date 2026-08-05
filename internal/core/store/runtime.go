package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"investgo/internal/common/errs"
	"investgo/internal/core"
)

type quoteRefreshResult struct {
	attemptedAt time.Time
	quotes      map[string]core.Quote
	problems    []string
	fxFetched   bool
	fxError     string
}

// Refresh refreshes real-time quotes and alert status, but does not touch historical trend cache.
// This allows the frontend to fetch charts on demand instead of repackaging historical data into the baseline snapshot on every refresh.
func (s *Store) Refresh(ctx context.Context, force bool) (core.StateSnapshot, error) {
	if !force {
		if cached, _, ok := s.refreshCache.Get("all"); ok {
			return cloneStateSnapshot(cached), nil
		}
	}

	// First copy current items slice to avoid holding read lock for extended period during network requests.
	s.mu.RLock()
	items := append([]core.WatchlistItem(nil), s.state.Items...)
	s.mu.RUnlock()

	result := s.refreshQuotesForItems(ctx, items)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.runtime.LastQuoteAttemptAt = ptrTime(result.attemptedAt)
	s.runtime.LastQuoteError = ""
	s.runtime.QuoteSource = s.quoteProviderSummaryLocked()

	if len(result.quotes) > 0 {
		// Match results by normalized target key to handle symbol format variations between user input and provider responses.
		for idx := range s.state.Items {
			target, err := core.ResolveQuoteTarget(s.state.Items[idx])
			if err != nil {
				continue
			}
			quote, ok := result.quotes[target.Key]
			if !ok {
				continue
			}
			applyQuoteToItem(&s.state.Items[idx], quote)
		}
		s.runtime.LastQuoteRefreshAt = ptrTime(time.Now())
	}

	fetchErr := errs.JoinProblems(result.problems)
	if fetchErr != nil {
		s.runtime.LastQuoteError = fetchErr.Error()
		s.logWarn("quotes", fmt.Sprintf("quote refresh failed: %v", fetchErr))
	}

	// Update FX rate runtime status.
	if result.fxError != "" {
		s.runtime.LastFxError = result.fxError
		s.logWarn("fx-rates", result.fxError)
	} else if result.fxFetched {
		s.runtime.LastFxError = ""
		s.runtime.LastFxRefreshAt = ptrTime(s.fxRates.ValidAt())
		s.logInfo("fx-rates", fmt.Sprintf("FX rates refreshed for %d currencies", s.fxRates.CurrencyCount()))
	}

	s.evaluateAlertsLocked()
	s.state.UpdatedAt = time.Now()
	// Price refreshes do not change portfolio structure, so only the quote-result caches
	// are invalidated. The history and overview caches remain valid across price ticks.
	s.invalidatePriceCachesLocked()
	// Live quotes are transient runtime state; persisting them on every price tick
	// blocks all readers under the write lock and adds disk I/O latency per refresh.
	// State is only restored from the last structural mutation (add/remove/update),
	// and live prices are re-fetched on startup, so refresh must not touch disk.

	snapshot := s.snapshotLocked()
	if fetchErr == nil && result.fxError == "" {
		s.refreshCache.Set("all", cloneStateSnapshot(snapshot), s.quoteRefreshTTLLocked())
	}
	return snapshot, nil
}

// RefreshItem refreshes only one tracked instrument so view-local refresh flows can avoid sending the full watchlist to upstream providers.
func (s *Store) RefreshItem(ctx context.Context, itemID string, force bool) (core.StateSnapshot, error) {
	if !force {
		if cached, _, ok := s.itemRefreshCache.Get(itemID); ok {
			snapshot := cloneStateSnapshot(cached)
			return snapshot, nil
		}
	}

	s.mu.RLock()
	index := s.findItemIndexLocked(itemID)
	if index == -1 {
		s.mu.RUnlock()
		return core.StateSnapshot{}, fmt.Errorf("Item not found: %s", itemID)
	}
	item := s.state.Items[index]
	s.mu.RUnlock()

	result := s.refreshQuotesForItems(ctx, []core.WatchlistItem{item})

	s.mu.Lock()
	defer s.mu.Unlock()

	s.runtime.LastQuoteAttemptAt = ptrTime(result.attemptedAt)
	s.runtime.LastQuoteError = ""
	s.runtime.QuoteSource = s.quoteProviderSummaryLocked()

	if target, err := core.ResolveQuoteTarget(item); err == nil {
		if quote, ok := result.quotes[target.Key]; ok {
			index = s.findItemIndexLocked(itemID)
			if index >= 0 {
				applyQuoteToItem(&s.state.Items[index], quote)
				s.runtime.LastQuoteRefreshAt = ptrTime(time.Now())
			}
		}
	}

	fetchErr := errs.JoinProblems(result.problems)
	if fetchErr != nil {
		s.runtime.LastQuoteError = fetchErr.Error()
		s.logWarn("quotes", fmt.Sprintf("quote refresh failed: %v", fetchErr))
	}

	if result.fxError != "" {
		s.runtime.LastFxError = result.fxError
		s.logWarn("fx-rates", result.fxError)
	} else if result.fxFetched {
		s.runtime.LastFxError = ""
		s.runtime.LastFxRefreshAt = ptrTime(s.fxRates.ValidAt())
		s.logInfo("fx-rates", fmt.Sprintf("FX rates refreshed for %d currencies", s.fxRates.CurrencyCount()))
	}

	s.evaluateAlertsLocked()
	s.state.UpdatedAt = time.Now()
	s.invalidatePriceCachesLocked()
	// See Refresh: live price ticks are not persisted; the last structural mutation
	// remains the on-disk state and prices are re-fetched on next startup.

	snapshot := s.snapshotLocked()
	if fetchErr == nil && result.fxError == "" {
		s.itemRefreshCache.Set(itemID, cloneStateSnapshot(snapshot), s.quoteRefreshTTLLocked())
	}
	return snapshot, nil
}

// refreshQuotesForItems batches items by their active market-specific provider so multi-market lists still respect per-market source settings.
func (s *Store) refreshQuotesForItems(ctx context.Context, items []core.WatchlistItem) quoteRefreshResult {
	result := quoteRefreshResult{
		attemptedAt: time.Now(),
		quotes:      map[string]core.Quote{},
	}

	// Resolve the active source/provider for every market under a single read lock
	// instead of re-locking per item; the provider pointers are safe to use
	// outside the lock because the provider map is immutable after startup.
	type batchGroup struct {
		sourceID string
		provider core.QuoteProvider
		items    []core.WatchlistItem
	}
	grouped := make(map[string]*batchGroup)
	s.mu.RLock()
	for _, item := range items {
		sourceID := s.activeQuoteSourceIDLocked(item.Market)
		provider := s.activeQuoteProviderLocked(item.Market)
		if provider == nil || sourceID == "" {
			continue
		}
		if g, ok := grouped[sourceID]; ok {
			g.items = append(g.items, item)
		} else {
			grouped[sourceID] = &batchGroup{
				sourceID: sourceID,
				provider: provider,
				items:    []core.WatchlistItem{item},
			}
		}
	}
	batchList := make([]*batchGroup, 0, len(grouped))
	for _, g := range grouped {
		batchList = append(batchList, g)
	}
	s.mu.RUnlock()

	// Refresh FX opportunistically alongside quote requests so derived dashboard values stay aligned after quote updates.
	if s.fxRates.IsStale() {
		if err := s.fxRates.Fetch(ctx); err != nil {
			result.fxError = err.Error()
		} else {
			result.fxFetched = true
		}
	}

	if len(batchList) == 0 {
		return result
	}

	// Run each market's provider concurrently: a mixed CN/HK/US watchlist no longer
	// waits for the slowest market sequentially, all providers run in parallel.
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, batch := range batchList {
		batch := batch
		wg.Add(1)
		go func() {
			defer wg.Done()
			batchQuotes, err := batch.provider.Fetch(ctx, batch.items)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.problems = append(result.problems, fmt.Sprintf("%s: %v", batch.provider.Name(), err))
			}
			for key, quote := range batchQuotes {
				result.quotes[key] = quote
			}
		}()
	}
	wg.Wait()

	return result
}

// ItemHistory queries historical price data for the specified item.
// Routing to the appropriate data source is handled by the historyProvider (HistoryRouter),
// which selects and sequences providers based on the item market and user settings.
func (s *Store) ItemHistory(
	ctx context.Context,
	itemID string,
	interval core.HistoryInterval,
	force bool,
) (core.HistorySeries, error) {
	cacheKey := itemID + "|" + string(interval)
	if !force {
		if cached, expiresAt, ok := s.historyCache.Get(cacheKey); ok {
			series := cloneHistorySeries(cached)
			series.Cached = true
			series.CacheExpiresAt = ptrTime(expiresAt)
			return series, nil
		}
	}

	s.mu.RLock()
	index := s.findItemIndexLocked(itemID)
	if index == -1 {
		s.mu.RUnlock()
		return core.HistorySeries{}, fmt.Errorf("Item not found: %s", itemID)
	}
	item := s.state.Items[index]
	s.mu.RUnlock()

	if s.historyProvider == nil {
		return core.HistorySeries{}, errors.New("History provider is not configured")
	}

	series, err := s.historyProvider.Fetch(ctx, item, interval)
	if err != nil {
		return core.HistorySeries{}, err
	}
	series.Snapshot = buildMarketSnapshot(decorateItemDerived(item), series)
	series.Cached = false
	// History OHLCV data is stable within each interval window; use a longer
	// per-interval TTL rather than the short HotCacheTTLSeconds setting.
	expiresAt := s.historyCache.Set(cacheKey, cloneHistorySeries(series), historyCacheTTLForInterval(interval))
	series.CacheExpiresAt = ptrTime(expiresAt)
	return series, nil
}

// OverviewAnalytics builds the overview analytics payload used by the dashboard overview module.
func (s *Store) OverviewAnalytics(ctx context.Context, force bool) (core.OverviewAnalytics, error) {
	s.mu.RLock()
	items := append([]core.WatchlistItem(nil), s.state.Items...)
	displayCurrency := s.state.Settings.DashboardCurrency
	// stateStamp guards against a race where a structural mutation (item add/remove/update,
	// settings change) happens concurrently with an in-flight overview build: if the result
	// is cached with an old stamp the next caller sees a miss and rebuilds. In the common
	// case invalidatePriceCachesLocked clears overviewCache after every price refresh, so
	// overviewCache.Get returns ok=false before this stamp comparison matters.
	stateStamp := s.holdingsUpdatedAt
	s.mu.RUnlock()
	if !force {
		if cached, expiresAt, ok := s.overviewCache.Get("all"); ok && cached.stateStamp.Equal(stateStamp) {
			analytics := cloneOverviewAnalytics(cached.analytics)
			analytics.Cached = true
			analytics.CacheExpiresAt = ptrTime(expiresAt)
			return analytics, nil
		}
	}

	relevantItems := make([]core.WatchlistItem, 0, len(items))
	for _, item := range items {
		if item.Quantity > 0 || len(validOverviewDCAEntries(item.DCAEntries)) > 0 {
			relevantItems = append(relevantItems, item)
		}
	}

	// Route history through ItemHistory so the shared historyCache is used.
	// This makes overview rebuilds cheap (no extra network calls) when history
	// is already cached, which is the common case after the user loads the chart
	// for any holding. Bypassing historyProvider.Fetch directly would circumvent
	// the cache and issue redundant network requests on every price refresh.
	calculator := newOverviewCalculator(
		s.fxRates, displayCurrency,
		func(ctx context.Context, item core.WatchlistItem, interval core.HistoryInterval) (core.HistorySeries, error) {
			return s.ItemHistory(ctx, item.ID, interval, false)
		})

	analytics, err := calculator.Build(ctx, relevantItems)
	if err != nil {
		return core.OverviewAnalytics{}, err
	}
	analytics.Cached = false

	expiresAt := s.overviewCache.Set("all", cachedOverviewValue{
		analytics:  cloneOverviewAnalytics(analytics),
		stateStamp: stateStamp,
	}, s.derivedCacheTTL())
	analytics.CacheExpiresAt = ptrTime(expiresAt)

	return analytics, nil
}
