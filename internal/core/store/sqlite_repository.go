package store

import (
	"database/sql"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"investgo/internal/core"
	"investgo/internal/core/instrument"
)

// SQLiteRepository persists InvestGo state in normalized SQLite tables.
type SQLiteRepository struct {
	db   *sql.DB
	path string
}

// NewSQLiteRepository creates a state repository backed by an initialized
// SQLite database. The caller retains ownership of db and must close it.
func NewSQLiteRepository(db *sql.DB, path string) Repository {
	return &SQLiteRepository{db: db, path: path}
}

// Path returns the SQLite database path displayed in runtime diagnostics.
func (r *SQLiteRepository) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

// Load reconstructs persisted application state from normalized SQLite tables.
func (r *SQLiteRepository) Load() (PersistedState, bool, error) {
	if r == nil || r.db == nil {
		return PersistedState{}, false, errors.New("sqlite state repository is not configured")
	}

	state, found, err := r.loadMetadata()
	if err != nil || !found {
		return state, found, err
	}
	if err := r.loadSettings(&state); err != nil {
		return PersistedState{}, false, err
	}
	itemIndex, err := r.loadItems(&state)
	if err != nil {
		return PersistedState{}, false, err
	}
	if err := r.loadDCAEntries(&state, itemIndex); err != nil {
		return PersistedState{}, false, err
	}
	if err := r.loadAlerts(&state); err != nil {
		return PersistedState{}, false, err
	}
	return state, true, nil
}

// Save replaces the current user state in one transaction while preserving
// catalog and pool rows that are not owned by the compatibility Store.
func (r *SQLiteRepository) Save(state PersistedState) error {
	if r == nil || r.db == nil {
		return errors.New("sqlite state repository is not configured")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin sqlite state save: %w", err)
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck

	if err := clearPersistedState(tx); err != nil {
		return err
	}
	if err := saveSettings(tx, state.Settings, state.UpdatedAt); err != nil {
		return err
	}
	for index, item := range state.Items {
		instrumentID, err := saveInstrument(tx, item, state.UpdatedAt)
		if err != nil {
			return err
		}
		if err := saveWatchlistItem(tx, instrumentID, item, index); err != nil {
			return err
		}
		if err := saveDCAEntries(tx, item); err != nil {
			return err
		}
	}
	for index, alert := range state.Alerts {
		if err := saveAlert(tx, alert, index); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		"INSERT INTO app_state_metadata(id, initialized_at, updated_at) VALUES (1, ?, ?)",
		formatTime(state.UpdatedAt),
		formatTime(state.UpdatedAt),
	); err != nil {
		return fmt.Errorf("save sqlite state metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite state save: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) loadMetadata() (PersistedState, bool, error) {
	var updatedAtRaw string
	err := r.db.QueryRow("SELECT updated_at FROM app_state_metadata WHERE id = 1").Scan(&updatedAtRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return PersistedState{}, false, nil
	}
	if err != nil {
		return PersistedState{}, false, fmt.Errorf("load sqlite state metadata: %w", err)
	}

	updatedAt, err := parseTime(updatedAtRaw)
	if err != nil {
		return PersistedState{}, false, fmt.Errorf("load sqlite state updated time: %w", err)
	}
	return PersistedState{UpdatedAt: updatedAt}, true, nil
}

func (r *SQLiteRepository) loadSettings(state *PersistedState) error {
	var developerMode int
	var useNativeTitleBar int
	err := r.db.QueryRow(`
        SELECT hot_cache_ttl_seconds, cn_quote_source, hk_quote_source, us_quote_source,
               theme_mode, color_theme, font_preset, amount_display, currency_display,
               price_color_scheme, locale, proxy_mode, proxy_url, alpha_vantage_api_key,
               twelve_data_api_key, finnhub_api_key, tiingo_api_key, polygon_api_key,
               developer_mode, dashboard_currency, use_native_title_bar
        FROM settings
        WHERE id = 1
    `).Scan(
		&state.Settings.HotCacheTTLSeconds,
		&state.Settings.CNQuoteSource,
		&state.Settings.HKQuoteSource,
		&state.Settings.USQuoteSource,
		&state.Settings.ThemeMode,
		&state.Settings.ColorTheme,
		&state.Settings.FontPreset,
		&state.Settings.AmountDisplay,
		&state.Settings.CurrencyDisplay,
		&state.Settings.PriceColorScheme,
		&state.Settings.Locale,
		&state.Settings.ProxyMode,
		&state.Settings.ProxyURL,
		&state.Settings.AlphaVantageAPIKey,
		&state.Settings.TwelveDataAPIKey,
		&state.Settings.FinnhubAPIKey,
		&state.Settings.TiingoAPIKey,
		&state.Settings.PolygonAPIKey,
		&developerMode,
		&state.Settings.DashboardCurrency,
		&useNativeTitleBar,
	)
	if err != nil {
		return fmt.Errorf("load sqlite settings: %w", err)
	}
	state.Settings.DeveloperMode = developerMode != 0
	state.Settings.UseNativeTitleBar = useNativeTitleBar != 0
	return nil
}

func (r *SQLiteRepository) loadItems(state *PersistedState) (map[string]int, error) {
	rows, err := r.db.Query(`
        SELECT w.id, i.symbol, i.name, i.display_name, i.market, i.quote_currency,
               w.quantity, w.cost_price, w.acquired_at, w.current_price,
               w.previous_close, w.open_price, w.day_high, w.day_low,
               w.change_value, w.change_percent, w.quote_source,
               w.quote_updated_at, w.pinned_at, w.thesis, w.tags_json, w.updated_at
        FROM watchlist_entries AS w
        JOIN instruments AS i ON i.id = w.instrument_id
        ORDER BY w.sort_order, w.id
    `)
	if err != nil {
		return nil, fmt.Errorf("query sqlite watchlist items: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	state.Items = []core.WatchlistItem{}
	itemIndex := make(map[string]int)
	for rows.Next() {
		var item core.WatchlistItem
		var officialName string
		var displayName string
		var quantityRaw string
		var costPriceRaw string
		var acquiredAtRaw sql.NullString
		var quoteUpdatedAtRaw sql.NullString
		var pinnedAtRaw sql.NullString
		var tagsJSON string
		var updatedAtRaw string
		if err := rows.Scan(
			&item.ID,
			&item.Symbol,
			&officialName,
			&displayName,
			&item.Market,
			&item.Currency,
			&quantityRaw,
			&costPriceRaw,
			&acquiredAtRaw,
			&item.CurrentPrice,
			&item.PreviousClose,
			&item.OpenPrice,
			&item.DayHigh,
			&item.DayLow,
			&item.Change,
			&item.ChangePercent,
			&item.QuoteSource,
			&quoteUpdatedAtRaw,
			&pinnedAtRaw,
			&item.Thesis,
			&tagsJSON,
			&updatedAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan sqlite watchlist item: %w", err)
		}

		item.Quantity, err = parseFloat(quantityRaw)
		if err != nil {
			return nil, fmt.Errorf("load quantity for item %s: %w", item.ID, err)
		}
		item.CostPrice, err = parseFloat(costPriceRaw)
		if err != nil {
			return nil, fmt.Errorf("load cost price for item %s: %w", item.ID, err)
		}
		item.AcquiredAt, err = parseOptionalTime(acquiredAtRaw)
		if err != nil {
			return nil, fmt.Errorf("load acquisition time for item %s: %w", item.ID, err)
		}
		item.QuoteUpdatedAt, err = parseOptionalTime(quoteUpdatedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("load quote time for item %s: %w", item.ID, err)
		}
		item.PinnedAt, err = parseOptionalTime(pinnedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("load pin time for item %s: %w", item.ID, err)
		}
		item.UpdatedAt, err = parseTime(updatedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("load update time for item %s: %w", item.ID, err)
		}
		if err := json.Unmarshal([]byte(tagsJSON), &item.Tags); err != nil {
			return nil, fmt.Errorf("load tags for item %s: %w", item.ID, err)
		}

		item.Name = officialName
		if strings.TrimSpace(displayName) != "" {
			item.Name = displayName
			item.DefaultName = officialName
			item.HasCustomName = true
		}

		itemIndex[item.ID] = len(state.Items)
		state.Items = append(state.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite watchlist items: %w", err)
	}
	return itemIndex, nil
}

func (r *SQLiteRepository) loadDCAEntries(state *PersistedState, itemIndex map[string]int) error {
	rows, err := r.db.Query(`
        SELECT watchlist_entry_id, id, entry_date, amount, shares, price, fee, note
        FROM dca_entries
        ORDER BY watchlist_entry_id, sort_order, id
    `)
	if err != nil {
		return fmt.Errorf("query sqlite DCA entries: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var itemID string
		var entry core.DCAEntry
		var dateRaw string
		var amountRaw string
		var sharesRaw string
		var priceRaw sql.NullString
		var feeRaw sql.NullString
		if err := rows.Scan(
			&itemID,
			&entry.ID,
			&dateRaw,
			&amountRaw,
			&sharesRaw,
			&priceRaw,
			&feeRaw,
			&entry.Note,
		); err != nil {
			return fmt.Errorf("scan sqlite DCA entry: %w", err)
		}

		index, exists := itemIndex[itemID]
		if !exists {
			return fmt.Errorf("load sqlite DCA entry %s: unknown watchlist item %s", entry.ID, itemID)
		}
		entry.Date, err = parseTime(dateRaw)
		if err != nil {
			return fmt.Errorf("load date for DCA entry %s: %w", entry.ID, err)
		}
		entry.Amount, err = parseFloat(amountRaw)
		if err != nil {
			return fmt.Errorf("load amount for DCA entry %s: %w", entry.ID, err)
		}
		entry.Shares, err = parseFloat(sharesRaw)
		if err != nil {
			return fmt.Errorf("load shares for DCA entry %s: %w", entry.ID, err)
		}
		entry.Price, err = parseOptionalFloat(priceRaw)
		if err != nil {
			return fmt.Errorf("load price for DCA entry %s: %w", entry.ID, err)
		}
		entry.Fee, err = parseOptionalFloat(feeRaw)
		if err != nil {
			return fmt.Errorf("load fee for DCA entry %s: %w", entry.ID, err)
		}
		state.Items[index].DCAEntries = append(state.Items[index].DCAEntries, entry)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sqlite DCA entries: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) loadAlerts(state *PersistedState) error {
	rows, err := r.db.Query(`
        SELECT id, watchlist_entry_id, name, condition, threshold, enabled,
               triggered, last_triggered_at, updated_at
        FROM alerts
        ORDER BY sort_order, id
    `)
	if err != nil {
		return fmt.Errorf("query sqlite alerts: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	state.Alerts = []core.AlertRule{}
	for rows.Next() {
		var alert core.AlertRule
		var thresholdRaw string
		var enabled int
		var triggered int
		var lastTriggeredAtRaw sql.NullString
		var updatedAtRaw string
		if err := rows.Scan(
			&alert.ID,
			&alert.ItemID,
			&alert.Name,
			&alert.Condition,
			&thresholdRaw,
			&enabled,
			&triggered,
			&lastTriggeredAtRaw,
			&updatedAtRaw,
		); err != nil {
			return fmt.Errorf("scan sqlite alert: %w", err)
		}

		alert.Threshold, err = parseFloat(thresholdRaw)
		if err != nil {
			return fmt.Errorf("load threshold for alert %s: %w", alert.ID, err)
		}
		alert.Enabled = enabled != 0
		alert.Triggered = triggered != 0
		alert.LastTriggeredAt, err = parseOptionalTime(lastTriggeredAtRaw)
		if err != nil {
			return fmt.Errorf("load trigger time for alert %s: %w", alert.ID, err)
		}
		alert.UpdatedAt, err = parseTime(updatedAtRaw)
		if err != nil {
			return fmt.Errorf("load update time for alert %s: %w", alert.ID, err)
		}
		state.Alerts = append(state.Alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sqlite alerts: %w", err)
	}
	return nil
}

func clearPersistedState(tx *sql.Tx) error {
	statements := []string{
		"DELETE FROM alerts",
		"DELETE FROM dca_entries",
		"DELETE FROM watchlist_entries",
		"DELETE FROM settings",
		"DELETE FROM app_state_metadata",
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("clear sqlite state with %q: %w", statement, err)
		}
	}
	return nil
}

func saveSettings(tx *sql.Tx, settings core.AppSettings, updatedAt time.Time) error {
	_, err := tx.Exec(`
        INSERT INTO settings(
            id, hot_cache_ttl_seconds, cn_quote_source, hk_quote_source, us_quote_source,
            theme_mode, color_theme, font_preset, amount_display, currency_display,
            price_color_scheme, locale, proxy_mode, proxy_url, alpha_vantage_api_key,
            twelve_data_api_key, finnhub_api_key, tiingo_api_key, polygon_api_key,
            developer_mode, dashboard_currency, use_native_title_bar, updated_at
        ) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
		settings.HotCacheTTLSeconds,
		settings.CNQuoteSource,
		settings.HKQuoteSource,
		settings.USQuoteSource,
		settings.ThemeMode,
		settings.ColorTheme,
		settings.FontPreset,
		settings.AmountDisplay,
		settings.CurrencyDisplay,
		settings.PriceColorScheme,
		settings.Locale,
		settings.ProxyMode,
		settings.ProxyURL,
		settings.AlphaVantageAPIKey,
		settings.TwelveDataAPIKey,
		settings.FinnhubAPIKey,
		settings.TiingoAPIKey,
		settings.PolygonAPIKey,
		boolInt(settings.DeveloperMode),
		settings.DashboardCurrency,
		boolInt(settings.UseNativeTitleBar),
		formatTime(updatedAt),
	)
	if err != nil {
		return fmt.Errorf("save sqlite settings: %w", err)
	}
	return nil
}

// saveInstrument stores the item's instrument row and returns the row id that
// watchlist entries must reference. Identity is canonicalized the same way as
// the instrument catalog so seeded HK/CN rows are reused instead of splitting
// into a second exchange-empty duplicate.
func saveInstrument(tx *sql.Tx, item core.WatchlistItem, stateUpdatedAt time.Time) (string, error) {
	officialName := item.Name
	displayName := ""
	if item.HasCustomName {
		displayName = item.Name
		if strings.TrimSpace(item.DefaultName) != "" {
			officialName = item.DefaultName
		}
	}
	normalized, err := instrument.Normalize(instrument.Instrument{
		AssetClass:    instrument.AssetClass(sqliteAssetClass(item.Market)),
		Symbol:        item.Symbol,
		Name:          officialName,
		DisplayName:   displayName,
		Market:        item.Market,
		QuoteCurrency: item.Currency,
	})
	if err != nil {
		return "", fmt.Errorf("normalize sqlite instrument for item %s: %w", item.ID, err)
	}
	instrumentID, err := instrument.CanonicalID(normalized.Identity())
	if err != nil {
		return "", fmt.Errorf("canonical id for item %s: %w", item.ID, err)
	}
	updatedAt := item.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = stateUpdatedAt
	}
	_, err = tx.Exec(`
        INSERT INTO instruments(
            id, asset_class, symbol, name, display_name, market, exchange, base_asset,
            quote_currency, status, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, 'active', ?, ?)
        ON CONFLICT(asset_class, market, exchange, symbol) DO UPDATE SET
            name = excluded.name,
            display_name = excluded.display_name,
            quote_currency = excluded.quote_currency,
            status = excluded.status,
            updated_at = excluded.updated_at
    `,
		instrumentID,
		normalized.AssetClass,
		normalized.Symbol,
		normalized.Name,
		normalized.DisplayName,
		normalized.Market,
		normalized.Exchange,
		normalized.QuoteCurrency,
		formatTime(updatedAt),
		formatTime(updatedAt),
	)
	if err != nil {
		return "", fmt.Errorf("save sqlite instrument for item %s: %w", item.ID, err)
	}

	var storedID string
	err = tx.QueryRow(`
        SELECT id
        FROM instruments
        WHERE asset_class = ? AND market = ? AND exchange = ? AND symbol = ?
    `, normalized.AssetClass, normalized.Market, normalized.Exchange, normalized.Symbol).Scan(&storedID)
	if err != nil {
		return "", fmt.Errorf("resolve sqlite instrument id for item %s: %w", item.ID, err)
	}
	return storedID, nil
}

func saveWatchlistItem(tx *sql.Tx, instrumentID string, item core.WatchlistItem, sortOrder int) error {
	// Keep a nil tag list as JSON null so a database round trip stays nil.
	// json/v2 would otherwise encode it as [] and reload a non-nil slice.
	tagsJSON, err := json.Marshal(item.Tags, json.FormatNilSliceAsNull(true))
	if err != nil {
		return fmt.Errorf("encode tags for item %s: %w", item.ID, err)
	}

	_, err = tx.Exec(`
        INSERT INTO watchlist_entries(
            id, instrument_id, quantity, cost_price, acquired_at, current_price,
            previous_close, open_price, day_high, day_low, change_value,
            change_percent, quote_source, quote_updated_at, pinned_at, thesis,
            tags_json, sort_order, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
		item.ID,
		instrumentID,
		formatFloat(item.Quantity),
		formatFloat(item.CostPrice),
		optionalTimeValue(item.AcquiredAt),
		item.CurrentPrice,
		item.PreviousClose,
		item.OpenPrice,
		item.DayHigh,
		item.DayLow,
		item.Change,
		item.ChangePercent,
		item.QuoteSource,
		optionalTimeValue(item.QuoteUpdatedAt),
		optionalTimeValue(item.PinnedAt),
		item.Thesis,
		string(tagsJSON),
		sortOrder,
		formatTime(item.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("save sqlite watchlist item %s: %w", item.ID, err)
	}
	return nil
}

func saveDCAEntries(tx *sql.Tx, item core.WatchlistItem) error {
	for index, entry := range item.DCAEntries {
		_, err := tx.Exec(`
            INSERT INTO dca_entries(
                id, watchlist_entry_id, entry_date, amount, shares, price, fee, note, sort_order
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        `,
			entry.ID,
			item.ID,
			formatTime(entry.Date),
			formatFloat(entry.Amount),
			formatFloat(entry.Shares),
			optionalFloatValue(entry.Price),
			optionalFloatValue(entry.Fee),
			entry.Note,
			index,
		)
		if err != nil {
			return fmt.Errorf("save sqlite DCA entry %s: %w", entry.ID, err)
		}
	}
	return nil
}

func saveAlert(tx *sql.Tx, alert core.AlertRule, sortOrder int) error {
	_, err := tx.Exec(`
        INSERT INTO alerts(
            id, watchlist_entry_id, name, condition, threshold, enabled,
            triggered, last_triggered_at, sort_order, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
		alert.ID,
		alert.ItemID,
		alert.Name,
		string(alert.Condition),
		formatFloat(alert.Threshold),
		boolInt(alert.Enabled),
		boolInt(alert.Triggered),
		optionalTimeValue(alert.LastTriggeredAt),
		sortOrder,
		formatTime(alert.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("save sqlite alert %s: %w", alert.ID, err)
	}
	return nil
}

func sqliteAssetClass(market string) string {
	normalized := strings.ToUpper(strings.TrimSpace(market))
	if strings.HasSuffix(normalized, "ETF") {
		return string(instrument.AssetClassETF)
	}
	if strings.Contains(normalized, "CRYPTO") {
		return string(instrument.AssetClassCrypto)
	}
	return string(instrument.AssetClassEquity)
}

func formatTime(value time.Time) string {
	return value.Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %q as RFC3339 time: %w", value, err)
	}
	return parsed, nil
}

func optionalTimeValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func parseOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func parseFloat(value string) (float64, error) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse decimal %q: %w", value, err)
	}
	return parsed, nil
}

func optionalFloatValue(value float64) any {
	if value == 0 {
		return nil
	}
	return formatFloat(value)
}

func parseOptionalFloat(value sql.NullString) (float64, error) {
	if !value.Valid {
		return 0, nil
	}
	return parseFloat(value.String)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
