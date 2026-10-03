package hot

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"investgo/internal/core/instrument"
	"investgo/internal/core/pool"
	sqlitestorage "investgo/internal/storage/sqlite"
)

func TestSeedBuiltInPoolsIsIdempotentAndUpdatesVersionedMembership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sqlitestorage.Open(filepath.Join(t.TempDir(), "investgo.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repositories := Repositories{
		Instruments: sqlitestorage.NewInstrumentRepository(db),
		Pools:       sqlitestorage.NewPoolRepository(db),
	}

	firstBaseline := []BuiltInPoolBaseline{
		{
			Pool: pool.Pool{
				ID:         pool.PoolIDUSSP500,
				Name:       "S&P 500",
				Market:     "US-STOCK",
				AssetClass: instrument.AssetClassEquity,
				Type:       pool.TypeIndex,
			},
			Members: []BuiltInPoolMember{
				{Symbol: "AAPL", Name: "Apple", Market: "US-STOCK", Currency: "USD"},
				{Symbol: "MSFT", Name: "Microsoft", Market: "US-STOCK", Currency: "USD"},
			},
		},
	}
	if err := seedBuiltInPools(ctx, repositories, "v1", firstBaseline); err != nil {
		t.Fatalf("seedBuiltInPools(v1) error = %v", err)
	}
	if err := seedBuiltInPools(ctx, repositories, "v1", firstBaseline); err != nil {
		t.Fatalf("seedBuiltInPools(v1 repeat) error = %v", err)
	}

	assertPoolMemberSymbols(t, db, pool.PoolIDUSSP500, []string{"AAPL", "MSFT"})

	aapl, found, err := repositories.Instruments.Find(ctx, instrument.Identity{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Symbol:     "AAPL",
	})
	if err != nil || !found {
		t.Fatalf("find AAPL = found %v, error %v", found, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pool_member_overrides(pool_id, instrument_id, action, created_at, updated_at)
		VALUES (?, ?, 'exclude', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')
	`, pool.PoolIDUSSP500, aapl.ID); err != nil {
		t.Fatalf("insert exclusion: %v", err)
	}

	secondBaseline := []BuiltInPoolBaseline{
		{
			Pool: firstBaseline[0].Pool,
			Members: []BuiltInPoolMember{
				{Symbol: "AAPL", Name: "Apple Inc.", Market: "US-STOCK", Currency: "USD"},
				{Symbol: "NVDA", Name: "NVIDIA", Market: "US-STOCK", Currency: "USD"},
			},
		},
	}
	if err := seedBuiltInPools(ctx, repositories, "v2", secondBaseline); err != nil {
		t.Fatalf("seedBuiltInPools(v2) error = %v", err)
	}

	assertPoolMemberSymbols(t, db, pool.PoolIDUSSP500, []string{"AAPL", "NVDA"})
	var overrideAction string
	if err := db.QueryRowContext(ctx, `
		SELECT action FROM pool_member_overrides WHERE pool_id = ? AND instrument_id = ?
	`, pool.PoolIDUSSP500, aapl.ID).Scan(&overrideAction); err != nil {
		t.Fatalf("load preserved exclusion: %v", err)
	}
	if overrideAction != "exclude" {
		t.Fatalf("override action = %q, want exclude", overrideAction)
	}
	var dataVersion string
	if err := db.QueryRowContext(ctx, "SELECT data_version FROM pools WHERE id = ?", pool.PoolIDUSSP500).
		Scan(&dataVersion); err != nil {
		t.Fatalf("load pool data version: %v", err)
	}
	if dataVersion != "v2" {
		t.Fatalf("data version = %q, want v2", dataVersion)
	}
}

func TestSeedBuiltInPoolsPreservesExistingInstrumentNames(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sqlitestorage.Open(filepath.Join(t.TempDir(), "investgo.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repositories := Repositories{
		Instruments: sqlitestorage.NewInstrumentRepository(db),
		Pools:       sqlitestorage.NewPoolRepository(db),
	}
	baseline := []BuiltInPoolBaseline{
		{
			Pool: pool.Pool{
				ID:         pool.PoolIDUSSP500,
				Name:       "S&P 500",
				Market:     "US-STOCK",
				AssetClass: instrument.AssetClassEquity,
				Type:       pool.TypeIndex,
			},
			Members: []BuiltInPoolMember{
				{Symbol: "AAPL", Name: "Apple", Market: "US-STOCK", Currency: "USD"},
			},
		},
	}
	if err := seedBuiltInPools(ctx, repositories, "v1", baseline); err != nil {
		t.Fatalf("seedBuiltInPools(v1) error = %v", err)
	}
	if _, err := repositories.Instruments.Upsert(ctx, instrument.Instrument{
		AssetClass:    instrument.AssetClassEquity,
		Market:        "US-STOCK",
		Symbol:        "AAPL",
		Name:          "My Apple",
		QuoteCurrency: "USD",
	}); err != nil {
		t.Fatalf("rename AAPL: %v", err)
	}

	baseline[0].Members[0].Name = "Apple Inc."
	if err := seedBuiltInPools(ctx, repositories, "v2", baseline); err != nil {
		t.Fatalf("seedBuiltInPools(v2) error = %v", err)
	}

	stored, found, err := repositories.Instruments.Find(ctx, instrument.Identity{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Symbol:     "AAPL",
	})
	if err != nil || !found {
		t.Fatalf("find AAPL = found %v, error %v", found, err)
	}
	if stored.Name != "My Apple" {
		t.Fatalf("AAPL name = %q, want preserved My Apple", stored.Name)
	}
}

func TestBuiltInPoolBaselinesExposeRequiredStablePoolsAndReturnCopies(t *testing.T) {
	t.Parallel()

	baselines := BuiltInPoolBaselines()
	required := map[string]bool{
		pool.PoolIDUSSP500:  false,
		pool.PoolIDUSNasdaq: false,
		pool.PoolIDUSDow:    false,
		pool.PoolIDUSETF:    false,
		pool.PoolIDCNA:      false,
		pool.PoolIDCNETF:    false,
		pool.PoolIDHK:       false,
		pool.PoolIDHKETF:    false,
	}
	for _, baseline := range baselines {
		if _, ok := required[baseline.Pool.ID]; ok {
			required[baseline.Pool.ID] = true
		}
		switch baseline.Pool.ID {
		case pool.PoolIDCNA, pool.PoolIDCNETF, pool.PoolIDHK, pool.PoolIDHKETF:
			if len(baseline.Members) == 0 {
				t.Errorf("pool %q has no default constituents", baseline.Pool.ID)
			}
		}
	}
	for id, found := range required {
		if !found {
			t.Errorf("required pool %q is missing", id)
		}
	}

	for index := range baselines {
		if len(baselines[index].Members) == 0 {
			continue
		}
		original := BuiltInPoolBaselines()
		originalSymbol := original[index].Members[0].Symbol
		baselines[index].Members[0].Symbol = "MUTATED"
		fresh := BuiltInPoolBaselines()
		if fresh[index].Members[0].Symbol != originalSymbol {
			t.Fatal("BuiltInPoolBaselines() returned mutable shared membership")
		}
		return
	}
	t.Fatal("no built-in baseline has members")
}

func TestSeedBuiltInPoolsLoadsProductionBaseline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sqlitestorage.Open(filepath.Join(t.TempDir(), "investgo.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if err := SeedBuiltInPools(ctx, Repositories{
		Instruments: sqlitestorage.NewInstrumentRepository(db),
		Pools:       sqlitestorage.NewPoolRepository(db),
	}, BuiltInPoolDataVersion); err != nil {
		t.Fatalf("SeedBuiltInPools() error = %v", err)
	}

	var poolCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pools").Scan(&poolCount); err != nil {
		t.Fatalf("count pools: %v", err)
	}
	if poolCount != len(BuiltInPoolBaselines()) {
		t.Fatalf("pool count = %d, want %d", poolCount, len(BuiltInPoolBaselines()))
	}
	var memberCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM builtin_pool_members").Scan(&memberCount); err != nil {
		t.Fatalf("count built-in members: %v", err)
	}
	if memberCount < 700 {
		t.Fatalf("built-in member count = %d, want at least 700", memberCount)
	}
	assertMinPoolMemberCount(t, db, pool.PoolIDCNA, 40)
	assertMinPoolMemberCount(t, db, pool.PoolIDHK, 40)
}

func assertMinPoolMemberCount(t *testing.T, db *sql.DB, poolID string, min int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM builtin_pool_members WHERE pool_id = ?
	`, poolID).Scan(&count); err != nil {
		t.Fatalf("count members for %s: %v", poolID, err)
	}
	if count < min {
		t.Fatalf("pool %s member count = %d, want at least %d", poolID, count, min)
	}
}

func assertPoolMemberSymbols(t *testing.T, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, poolID string, want []string) {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), `
		SELECT i.symbol
		FROM builtin_pool_members AS membership
		JOIN instruments AS i ON i.id = membership.instrument_id
		WHERE membership.pool_id = ?
		ORDER BY i.symbol
	`, poolID)
	if err != nil {
		t.Fatalf("query pool members: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var symbol string
		if err := rows.Scan(&symbol); err != nil {
			t.Fatalf("scan pool member: %v", err)
		}
		got = append(got, symbol)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pool members: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("pool member symbols = %v, want %v", got, want)
	}
}
