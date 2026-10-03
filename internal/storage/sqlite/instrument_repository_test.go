package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"investgo/internal/core/instrument"
)

func TestInstrumentRepositoryRoundTripAndCanonicalUniqueness(t *testing.T) {
	t.Parallel()

	db := openInstrumentRepositoryTestDB(t)
	repository := NewInstrumentRepository(db)
	ctx := context.Background()
	createdAt := time.Date(2026, time.August, 31, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))

	first, err := repository.Upsert(ctx, instrument.Instrument{
		AssetClass:    instrument.AssetClassEquity,
		Market:        "cn",
		Symbol:        "SH600519",
		Name:          "贵州茅台",
		QuoteCurrency: "cny",
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	})
	if err != nil {
		t.Fatalf("Upsert(first) error = %v", err)
	}
	if first.ID == "" {
		t.Fatal("Upsert(first).ID is empty")
	}
	if first.Symbol != "600519" || first.Market != "CN-A" || first.Exchange != "SSE" {
		t.Fatalf("Upsert(first) identity = %+v", first.Identity())
	}
	if !first.CreatedAt.Equal(createdAt) || first.CreatedAt.Location() != time.UTC {
		t.Fatalf("Upsert(first).CreatedAt = %v, want UTC %v", first.CreatedAt, createdAt.UTC())
	}

	second, err := repository.Upsert(ctx, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "A-share",
		Exchange:   "XSHG",
		Symbol:     "600519.SH",
		Name:       "Kweichow Moutai",
		Status:     instrument.InstrumentStatusInactive,
		UpdatedAt:  createdAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Upsert(second) error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("Upsert(second).ID = %q, want existing %q", second.ID, first.ID)
	}
	if second.Name != "Kweichow Moutai" || second.Status != instrument.InstrumentStatusInactive {
		t.Fatalf("Upsert(second) metadata = (%q, %q)", second.Name, second.Status)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("Upsert(second).CreatedAt = %v, want preserved %v", second.CreatedAt, first.CreatedAt)
	}

	byID, found, err := repository.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found {
		t.Fatal("Get() found = false")
	}
	assertStoredInstrumentEqual(t, byID, second)

	byIdentity, found, err := repository.Find(ctx, instrument.Identity{
		AssetClass: instrument.AssetClassEquity,
		Market:     "CN-A",
		Exchange:   "SH",
		Symbol:     "SH600519",
	})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if !found {
		t.Fatal("Find() found = false")
	}
	assertStoredInstrumentEqual(t, byIdentity, second)

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM instruments WHERE symbol = '600519'").
		Scan(&count); err != nil {
		t.Fatalf("count instruments: %v", err)
	}
	if count != 1 {
		t.Fatalf("instrument count = %d, want 1", count)
	}
}

func TestInstrumentRepositorySetDisplayNameSurvivesUpsert(t *testing.T) {
	t.Parallel()

	db := openInstrumentRepositoryTestDB(t)
	repository := NewInstrumentRepository(db)
	ctx := context.Background()

	stored, err := repository.Upsert(ctx, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Symbol:     "AAPL",
		Name:       "Apple",
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	aliased, err := repository.SetDisplayName(ctx, stored.ID, "苹果")
	if err != nil {
		t.Fatalf("SetDisplayName() error = %v", err)
	}
	if aliased.Name != "Apple" || aliased.Display() != "苹果" {
		t.Fatalf("aliased = %+v", aliased)
	}

	updated, err := repository.Upsert(ctx, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Symbol:     "AAPL",
		Name:       "Apple Inc.",
	})
	if err != nil {
		t.Fatalf("Upsert(second) error = %v", err)
	}
	if updated.Name != "Apple Inc." || updated.DisplayName != "苹果" || updated.Display() != "苹果" {
		t.Fatalf("upsert cleared alias: %+v", updated)
	}

	cleared, err := repository.SetDisplayName(ctx, stored.ID, "")
	if err != nil {
		t.Fatalf("SetDisplayName(reset) error = %v", err)
	}
	if cleared.DisplayName != "" || cleared.Display() != "Apple Inc." {
		t.Fatalf("cleared = %+v", cleared)
	}
}

func TestInstrumentRepositorySupportsMarketsAndAssetClasses(t *testing.T) {
	t.Parallel()

	db := openInstrumentRepositoryTestDB(t)
	repository := NewInstrumentRepository(db)
	ctx := context.Background()

	inputs := []instrument.Instrument{
		{AssetClass: instrument.AssetClassEquity, Market: "HK", Symbol: "700", Name: "Tencent"},
		{AssetClass: instrument.AssetClassEquity, Market: "US", Exchange: "NASDAQ", Symbol: "aapl", Name: "Apple"},
		{AssetClass: instrument.AssetClassETF, Market: "CN-ETF", Symbol: "510300.SH", Name: "沪深300ETF"},
		{AssetClass: instrument.AssetClassETF, Market: "HK-ETF", Symbol: "2800.HK", Name: "Tracker Fund"},
		{AssetClass: instrument.AssetClassETF, Market: "US ETF", Exchange: "ARCX", Symbol: "spy", Name: "SPDR S&P 500"},
	}

	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		stored, err := repository.Upsert(ctx, input)
		if err != nil {
			t.Fatalf("Upsert(%s) error = %v", input.Symbol, err)
		}
		if _, duplicate := seen[stored.ID]; duplicate {
			t.Fatalf("duplicate canonical ID %q for %+v", stored.ID, stored.Identity())
		}
		seen[stored.ID] = struct{}{}
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM instruments").Scan(&count); err != nil {
		t.Fatalf("count instruments: %v", err)
	}
	if count != len(inputs) {
		t.Fatalf("instrument count = %d, want %d", count, len(inputs))
	}
}

func TestInstrumentRepositoryProviderSymbolUpsertIsUniquePerProvider(t *testing.T) {
	t.Parallel()

	db := openInstrumentRepositoryTestDB(t)
	repository := NewInstrumentRepository(db)
	ctx := context.Background()

	stored, err := repository.Upsert(ctx, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Exchange:   "NASDAQ",
		Symbol:     "AAPL",
		Name:       "Apple",
	})
	if err != nil {
		t.Fatalf("Upsert(instrument) error = %v", err)
	}

	mapping, err := repository.UpsertProviderSymbol(ctx, instrument.ProviderSymbol{
		InstrumentID: stored.ID,
		ProviderID:   " Yahoo ",
		Symbol:       "AAPL",
		MetadataJSON: `{"exchange":"NMS"}`,
	})
	if err != nil {
		t.Fatalf("UpsertProviderSymbol(first) error = %v", err)
	}
	if mapping.ProviderID != "yahoo" {
		t.Fatalf("ProviderID = %q, want yahoo", mapping.ProviderID)
	}

	updated, err := repository.UpsertProviderSymbol(ctx, instrument.ProviderSymbol{
		InstrumentID: stored.ID,
		ProviderID:   "yahoo",
		Symbol:       "AAPL.US",
		MetadataJSON: `{"exchange":"NASDAQ"}`,
	})
	if err != nil {
		t.Fatalf("UpsertProviderSymbol(second) error = %v", err)
	}
	if updated.Symbol != "AAPL.US" {
		t.Fatalf("updated symbol = %q, want AAPL.US", updated.Symbol)
	}

	got, found, err := repository.GetProviderSymbol(ctx, stored.ID, " YAHOO ")
	if err != nil {
		t.Fatalf("GetProviderSymbol() error = %v", err)
	}
	if !found {
		t.Fatal("GetProviderSymbol() found = false")
	}
	if got != updated {
		t.Fatalf("GetProviderSymbol() = %+v, want %+v", got, updated)
	}

	if _, err := repository.UpsertProviderSymbol(ctx, instrument.ProviderSymbol{
		InstrumentID: stored.ID,
		ProviderID:   "tiingo",
		Symbol:       "AAPL",
	}); err != nil {
		t.Fatalf("UpsertProviderSymbol(tiingo) error = %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM provider_symbols WHERE instrument_id = ?", stored.ID).
		Scan(&count); err != nil {
		t.Fatalf("count provider symbols: %v", err)
	}
	if count != 2 {
		t.Fatalf("provider symbol count = %d, want 2", count)
	}

	if err := repository.DeleteProviderSymbol(ctx, stored.ID, "YAHOO"); err != nil {
		t.Fatalf("DeleteProviderSymbol() error = %v", err)
	}
	if _, found, err := repository.GetProviderSymbol(ctx, stored.ID, "yahoo"); err != nil || found {
		t.Fatalf("GetProviderSymbol(after delete) = found %v, error %v", found, err)
	}
}

func TestInstrumentRepositoryRejectsUnknownProviderInstrument(t *testing.T) {
	t.Parallel()

	db := openInstrumentRepositoryTestDB(t)
	repository := NewInstrumentRepository(db)
	_, err := repository.UpsertProviderSymbol(context.Background(), instrument.ProviderSymbol{
		InstrumentID: "missing-instrument",
		ProviderID:   "yahoo",
		Symbol:       "AAPL",
	})
	if err == nil {
		t.Fatal("UpsertProviderSymbol() error = nil")
	}
}

func openInstrumentRepositoryTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := Open(filepath.Join(t.TempDir(), "investgo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyMigrations(context.Background(), db); err != nil {
		t.Fatalf("ApplyMigrations() error = %v", err)
	}
	return db
}

func assertStoredInstrumentEqual(t *testing.T, got, want instrument.Instrument) {
	t.Helper()

	if got.ID != want.ID || got.Identity() != want.Identity() || got.Name != want.Name ||
		got.BaseAsset != want.BaseAsset || got.QuoteCurrency != want.QuoteCurrency || got.Status != want.Status ||
		!got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("instrument = %+v, want %+v", got, want)
	}
}
