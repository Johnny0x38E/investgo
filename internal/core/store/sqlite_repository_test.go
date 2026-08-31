package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"investgo/internal/core"
	"investgo/internal/core/hot"
	sqlitestorage "investgo/internal/storage/sqlite"
)

func TestSQLiteRepositoryLoadEmptyDatabase(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "investgo.db")
	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close sqlite database: %v", err)
		}
	})
	if err := sqlitestorage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatalf("apply sqlite migrations: %v", err)
	}

	repository := NewSQLiteRepository(db, databasePath)
	state, found, err := repository.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if found {
		t.Fatal("Load() found = true; want false for an uninitialized database")
	}
	if len(state.Items) != 0 || len(state.Alerts) != 0 {
		t.Fatalf("Load() state = %+v; want zero state", state)
	}
}

// TestSQLiteRepositorySaveReusesSeededCatalogInstruments simulates the real
// startup order: built-in pools are seeded into the shared catalog first, then
// the Store saves user state whose items may share a canonical instrument
// identity with those seeded rows. Saving must succeed and reference the
// existing catalog row instead of failing on the identity unique constraint.
func TestSQLiteRepositorySaveReusesSeededCatalogInstruments(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "investgo.db")
	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatalf("apply sqlite migrations: %v", err)
	}

	ctx := context.Background()
	instrumentRepository := sqlitestorage.NewInstrumentRepository(db)
	poolRepository := sqlitestorage.NewPoolRepository(db)
	if err := hot.SeedBuiltInPools(ctx, hot.Repositories{
		Instruments: instrumentRepository,
		Pools:       poolRepository,
	}, hot.BuiltInPoolDataVersion); err != nil {
		t.Fatalf("seed built-in pools: %v", err)
	}

	updatedAt := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	state := PersistedState{
		Items: []core.WatchlistItem{
			{
				ID:        "item-voo",
				Symbol:    "VOO",
				Name:      "Vanguard S&P 500 ETF",
				Market:    "US-ETF",
				Currency:  "USD",
				UpdatedAt: updatedAt,
			},
		},
		Alerts:    []core.AlertRule{},
		Settings:  core.AppSettings{ProxyMode: "system"},
		UpdatedAt: updatedAt,
	}
	repository := NewSQLiteRepository(db, databasePath)
	if err := repository.Save(state); err != nil {
		t.Fatalf("Save() after pool seeding error = %v", err)
	}

	var instrumentCount int
	if err := db.QueryRow(`
        SELECT COUNT(*)
        FROM instruments
        WHERE asset_class = 'etf' AND market = 'US-ETF' AND exchange = '' AND symbol = 'VOO'
    `).Scan(&instrumentCount); err != nil {
		t.Fatalf("count VOO rows: %v", err)
	}
	if instrumentCount != 1 {
		t.Fatalf("VOO instrument rows = %d; want 1 (seeded row reused)", instrumentCount)
	}

	got, found, err := repository.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !found || len(got.Items) != 1 || got.Items[0].Symbol != "VOO" {
		t.Fatalf("Load() = %+v, found %v; want VOO item", got, found)
	}
}

func TestSQLiteRepositorySaveReusesSeededHKAndCNIdentities(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "investgo.db")
	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatalf("apply sqlite migrations: %v", err)
	}

	ctx := context.Background()
	if err := hot.SeedBuiltInPools(ctx, hot.Repositories{
		Instruments: sqlitestorage.NewInstrumentRepository(db),
		Pools:       sqlitestorage.NewPoolRepository(db),
	}, hot.BuiltInPoolDataVersion); err != nil {
		t.Fatalf("seed built-in pools: %v", err)
	}

	updatedAt := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	state := PersistedState{
		Items: []core.WatchlistItem{
			{
				ID:        "item-2800",
				Symbol:    "2800.HK",
				Name:      "Tracker Fund of Hong Kong",
				Market:    "HK-ETF",
				Currency:  "HKD",
				UpdatedAt: updatedAt,
			},
			{
				ID:        "item-moutai",
				Symbol:    "600519.SH",
				Name:      "Kweichow Moutai",
				Market:    "CN-A",
				Currency:  "CNY",
				UpdatedAt: updatedAt,
			},
		},
		Alerts:    []core.AlertRule{},
		Settings:  core.AppSettings{ProxyMode: "system"},
		UpdatedAt: updatedAt,
	}
	repository := NewSQLiteRepository(db, databasePath)
	if err := repository.Save(state); err != nil {
		t.Fatalf("Save() after pool seeding error = %v", err)
	}

	assertSingleInstrument(t, db, "etf", "HK-ETF", "HKEX", "02800")
	assertSingleInstrument(t, db, "equity", "CN-A", "SSE", "600519")

	got, found, err := repository.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !found || len(got.Items) != 2 {
		t.Fatalf("Load() = %+v, found %v; want 2 items", got, found)
	}
}

func assertSingleInstrument(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}, assetClass, market, exchange, symbol string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`
        SELECT COUNT(*)
        FROM instruments
        WHERE asset_class = ? AND market = ? AND exchange = ? AND symbol = ?
    `, assetClass, market, exchange, symbol).Scan(&count); err != nil {
		t.Fatalf("count %s/%s: %v", market, symbol, err)
	}
	if count != 1 {
		t.Fatalf("%s/%s instrument rows = %d; want 1 (seeded row reused)", market, symbol, count)
	}
}

func TestSQLiteRepositoryRoundTripPreservesState(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "investgo.db")
	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close sqlite database: %v", err)
		}
	})
	if err := sqlitestorage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatalf("apply sqlite migrations: %v", err)
	}

	acquiredAt := time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC)
	quoteUpdatedAt := time.Date(2026, time.August, 31, 14, 3, 2, 123456789, time.UTC)
	pinnedAt := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	lastTriggeredAt := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, time.August, 31, 14, 5, 0, 0, time.UTC)

	want := PersistedState{
		Items: []core.WatchlistItem{
			{
				ID:             "item-aapl",
				Symbol:         "AAPL",
				Name:           "Apple",
				Market:         "US-STOCK",
				Currency:       "USD",
				Quantity:       1.25,
				CostPrice:      188.75,
				AcquiredAt:     &acquiredAt,
				CurrentPrice:   229.31,
				PreviousClose:  227.16,
				OpenPrice:      228.04,
				DayHigh:        230.12,
				DayLow:         226.91,
				Change:         2.15,
				ChangePercent:  0.9465,
				QuoteSource:    "Yahoo",
				QuoteUpdatedAt: &quoteUpdatedAt,
				PinnedAt:       &pinnedAt,
				Thesis:         "Services growth and cash generation",
				Tags:           []string{"technology", "quality"},
				DCAEntries: []core.DCAEntry{
					{
						ID:     "dca-aapl-1",
						Date:   time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
						Amount: 250.5,
						Shares: 1.125,
						Price:  220.9,
						Fee:    1.99,
						Note:   "first scheduled purchase",
					},
					{
						ID:     "dca-aapl-2",
						Date:   time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
						Amount: 200,
						Shares: 0.875,
						Note:   "price derived from amount",
					},
				},
				UpdatedAt: updatedAt.Add(-time.Minute),
			},
			{
				ID:            "item-qqq",
				Symbol:        "QQQ",
				Name:          "Invesco QQQ Trust",
				Market:        "US-ETF",
				Currency:      "USD",
				CurrentPrice:  578.42,
				PreviousClose: 576.8,
				Change:        1.62,
				ChangePercent: 0.2809,
				QuoteSource:   "EastMoney",
				Thesis:        "Watch-only ETF",
				Tags:          nil,
				DCAEntries:    nil,
				UpdatedAt:     updatedAt.Add(-2 * time.Minute),
			},
		},
		Alerts: []core.AlertRule{
			{
				ID:              "alert-aapl",
				ItemID:          "item-aapl",
				Name:            "AAPL breakout",
				Condition:       core.AlertAbove,
				Threshold:       235.5,
				Enabled:         true,
				Triggered:       true,
				LastTriggeredAt: &lastTriggeredAt,
				UpdatedAt:       updatedAt.Add(-30 * time.Second),
			},
		},
		Settings: core.AppSettings{
			HotCacheTTLSeconds: 90,
			CNQuoteSource:      "sina",
			HKQuoteSource:      "xueqiu",
			USQuoteSource:      "yahoo",
			ThemeMode:          "dark",
			ColorTheme:         "forest",
			FontPreset:         "compact",
			AmountDisplay:      "full",
			CurrencyDisplay:    "code",
			PriceColorScheme:   "intl",
			Locale:             "zh-CN",
			ProxyMode:          "custom",
			ProxyURL:           "http://127.0.0.1:7890",
			AlphaVantageAPIKey: "alpha-key",
			TwelveDataAPIKey:   "twelve-key",
			FinnhubAPIKey:      "finnhub-key",
			TiingoAPIKey:       "tiingo-key",
			PolygonAPIKey:      "polygon-key",
			DeveloperMode:      true,
			DashboardCurrency:  "CNY",
			UseNativeTitleBar:  true,
		},
		UpdatedAt: updatedAt,
	}

	repository := NewSQLiteRepository(db, databasePath)
	if err := repository.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, found, err := repository.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !found {
		t.Fatal("Load() found = false; want true after Save()")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch\ngot:  %#v\nwant: %#v", got, want)
	}

	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatalf("foreign_key_check query: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign_key_check returned a violation")
	}
}

func TestSQLiteRepositorySecondSaveReplacesChildRows(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "investgo.db")
	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatalf("apply sqlite migrations: %v", err)
	}
	repository := NewSQLiteRepository(db, databasePath)

	firstTime := time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC)
	first := PersistedState{
		Items: []core.WatchlistItem{
			{
				ID:        "item-aapl",
				Symbol:    "AAPL",
				Name:      "Apple",
				Market:    "US-STOCK",
				Currency:  "USD",
				Tags:      []string{"first"},
				UpdatedAt: firstTime,
				DCAEntries: []core.DCAEntry{
					{ID: "dca-old-1", Date: firstTime, Amount: 100, Shares: 0.5},
					{ID: "dca-old-2", Date: firstTime.Add(time.Hour), Amount: 120, Shares: 0.6},
				},
			},
		},
		Alerts: []core.AlertRule{
			{
				ID:        "alert-old",
				ItemID:    "item-aapl",
				Name:      "old alert",
				Condition: core.AlertAbove,
				Threshold: 250,
				Enabled:   true,
				UpdatedAt: firstTime,
			},
		},
		Settings:  core.AppSettings{ProxyMode: "system"},
		UpdatedAt: firstTime,
	}
	if err := repository.Save(first); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}

	secondTime := firstTime.Add(24 * time.Hour)
	second := PersistedState{
		Items: []core.WatchlistItem{
			{
				ID:        "item-aapl",
				Symbol:    "AAPL",
				Name:      "Apple Inc.",
				Market:    "US-STOCK",
				Currency:  "USD",
				Tags:      []string{"updated"},
				UpdatedAt: secondTime,
				DCAEntries: []core.DCAEntry{
					{ID: "dca-new", Date: secondTime, Amount: 150, Shares: 0.7},
				},
			},
		},
		Alerts:    []core.AlertRule{},
		Settings:  core.AppSettings{ProxyMode: "none", DashboardCurrency: "USD"},
		UpdatedAt: secondTime,
	}
	if err := repository.Save(second); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	got, found, err := repository.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !found {
		t.Fatal("Load() found = false; want true")
	}
	if !reflect.DeepEqual(got, second) {
		t.Fatalf("second save mismatch\ngot:  %#v\nwant: %#v", got, second)
	}

	for table, wantCount := range map[string]int{"watchlist_entries": 1, "dca_entries": 1, "alerts": 0} {
		var gotCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&gotCount); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if gotCount != wantCount {
			t.Fatalf("%s count = %d; want %d", table, gotCount, wantCount)
		}
	}
}

func TestSQLiteRepositoryFailedSaveRollsBackPreviousState(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "investgo.db")
	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatalf("apply sqlite migrations: %v", err)
	}
	repository := NewSQLiteRepository(db, databasePath)

	updatedAt := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	previous := PersistedState{
		Items: []core.WatchlistItem{
			{
				ID:        "item-aapl",
				Symbol:    "AAPL",
				Name:      "Apple",
				Market:    "US-STOCK",
				Currency:  "USD",
				UpdatedAt: updatedAt,
			},
		},
		Alerts:    []core.AlertRule{},
		Settings:  core.AppSettings{ProxyMode: "system"},
		UpdatedAt: updatedAt,
	}
	if err := repository.Save(previous); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}

	invalid := clonePersistedState(previous)
	invalid.Items = append(invalid.Items, core.WatchlistItem{
		ID:        "item-aapl-duplicate",
		Symbol:    "AAPL",
		Name:      "Duplicate canonical listing",
		Market:    "US-STOCK",
		Currency:  "USD",
		UpdatedAt: updatedAt.Add(time.Minute),
	})
	invalid.UpdatedAt = updatedAt.Add(time.Minute)
	if err := repository.Save(invalid); err == nil {
		t.Fatal("Save() invalid duplicate error = nil; want constraint failure")
	}

	got, found, err := repository.Load()
	if err != nil {
		t.Fatalf("Load() after failed save error = %v", err)
	}
	if !found {
		t.Fatal("Load() after failed save found = false; want true")
	}
	if !reflect.DeepEqual(got, previous) {
		t.Fatalf("rollback mismatch\ngot:  %#v\nwant: %#v", got, previous)
	}
}
