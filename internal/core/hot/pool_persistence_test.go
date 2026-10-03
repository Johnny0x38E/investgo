package hot

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"investgo/internal/core"
	"investgo/internal/core/instrument"
	"investgo/internal/core/pool"
	sqlitestorage "investgo/internal/storage/sqlite"
)

// TestEmptyPoolBrowseReturnsNonNullEmptyItems guards the frontend contract:
// a pool-backed category with no members must serialize as `items: []`, never
// `items: null`, because the frontend spreads the array during render.
func TestEmptyPoolBrowseReturnsNonNullEmptyItems(t *testing.T) {
	t.Parallel()

	ctx, _, catalog, pools := newHotPoolTestRepositories(t)
	if _, err := pools.Upsert(ctx, pool.Pool{
		ID: pool.PoolIDUSSP500, Name: "S&P 500", Market: "US-STOCK",
		AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex,
	}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}

	hotService := NewHotService(nil, nil, nil, pool.NewService(catalog, pools))
	response, err := hotService.List(ctx, core.HotCategoryUSSP500, core.HotSortVolume, "", 1, 20, HotListOptions{
		USQuoteSource: "yahoo",
	})
	if err != nil {
		t.Fatalf("List(us-sp500) error = %v", err)
	}
	if response.Items == nil {
		t.Fatal("List(us-sp500).Items = nil; want non-nil empty slice (JSON [] not null)")
	}
	if len(response.Items) != 0 || response.Total != 0 || response.HasMore {
		t.Fatalf("List(us-sp500) = %+v; want empty page without more", response)
	}
}

func TestExcludedBuiltInUSMemberDisappearsAfterPoolServiceRestart(t *testing.T) {
	t.Parallel()

	ctx, _, catalog, pools := newHotPoolTestRepositories(t)
	if _, err := pools.Upsert(ctx, pool.Pool{
		ID: pool.PoolIDUSSP500, Name: "S&P 500", Market: "US-STOCK",
		AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex,
	}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	aapl := upsertHotTestInstrument(t, ctx, catalog, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity, Market: "US-STOCK", Symbol: "AAPL", Name: "Apple",
	})
	msft := upsertHotTestInstrument(t, ctx, catalog, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity, Market: "US-STOCK", Symbol: "MSFT", Name: "Microsoft",
	})
	if err := pools.ReplaceBuiltInMembers(ctx, pool.PoolIDUSSP500, "v1", []string{aapl.ID, msft.ID}); err != nil {
		t.Fatalf("replace baseline: %v", err)
	}
	if _, err := pool.NewService(catalog, pools).ExcludeMember(ctx, pool.PoolIDUSSP500, aapl.ID); err != nil {
		t.Fatalf("exclude AAPL: %v", err)
	}

	restartedPoolService := pool.NewService(catalog, pools)
	hotService := NewHotService(nil, nil, nil, restartedPoolService)
	hotService.poolQuoteFn = func(_ context.Context, seeds []hotSeed, _ string) ([]core.HotItem, error) {
		items := make([]core.HotItem, 0, len(seeds))
		for _, seed := range seeds {
			items = append(items, core.HotItem{
				Symbol: seed.Symbol, Name: seed.Name, Market: seed.Market, Currency: seed.Currency,
			})
		}
		return items, nil
	}

	response, err := hotService.browsePoolCategory(
		ctx,
		core.HotCategoryUSSP500,
		core.HotSortVolume,
		1,
		20,
		HotListOptions{
			USQuoteSource: "yahoo",
			BypassCache:   true,
		},
	)
	if err != nil {
		t.Fatalf("browsePoolCategory() error = %v", err)
	}
	if response.Total != 1 || len(response.Items) != 1 || response.Items[0].Symbol != "MSFT" {
		t.Fatalf("response = %+v, want only MSFT", response)
	}
}

func TestExcludedCNAMemberDisappearsFromPoolBrowse(t *testing.T) {
	t.Parallel()

	ctx, _, catalog, pools := newHotPoolTestRepositories(t)
	if _, err := pools.Upsert(ctx, pool.Pool{
		ID: pool.PoolIDCNA, Name: "A-shares", Market: "CN-A",
		AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex,
	}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	moutai := upsertHotTestInstrument(t, ctx, catalog, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity, Market: "CN-A", Symbol: "600519.SH", Name: "贵州茅台",
	})
	pingan := upsertHotTestInstrument(t, ctx, catalog, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity, Market: "CN-A", Symbol: "000001.SZ", Name: "平安银行",
	})
	if err := pools.ReplaceBuiltInMembers(ctx, pool.PoolIDCNA, "v1", []string{moutai.ID, pingan.ID}); err != nil {
		t.Fatalf("replace baseline: %v", err)
	}
	if _, err := pool.NewService(catalog, pools).ExcludeMember(ctx, pool.PoolIDCNA, moutai.ID); err != nil {
		t.Fatalf("exclude 贵州茅台: %v", err)
	}

	hotService := NewHotService(nil, nil, nil, pool.NewService(catalog, pools))
	hotService.poolQuoteFn = func(_ context.Context, seeds []hotSeed, _ string) ([]core.HotItem, error) {
		items := make([]core.HotItem, 0, len(seeds))
		for _, seed := range seeds {
			items = append(items, core.HotItem{
				Symbol: seed.Symbol, Name: seed.Name, Market: seed.Market, Currency: seed.Currency,
			})
		}
		return items, nil
	}

	response, err := hotService.browsePoolCategory(ctx, core.HotCategoryCNA, core.HotSortVolume, 1, 20, HotListOptions{
		CNQuoteSource: "sina",
		BypassCache:   true,
	})
	if err != nil {
		t.Fatalf("browsePoolCategory() error = %v", err)
	}
	if response.Total != 1 || len(response.Items) != 1 || response.Items[0].Symbol != "000001.SZ" {
		t.Fatalf("response = %+v, want only 平安银行", response)
	}
}

func TestInvalidatePoolClearsOnlyMappedCategoryCaches(t *testing.T) {
	t.Parallel()

	hotService := NewHotService(nil, nil, nil)
	ttl := time.Minute
	hotService.responseCache.Set("us-sp500|volume||1|20|yahoo", core.HotListResponse{}, ttl)
	hotService.responseCache.Set("us-nasdaq|volume||1|20|yahoo", core.HotListResponse{}, ttl)
	hotService.rankCache.Set("us-sp500|yahoo", []core.HotItem{{Symbol: "AAPL"}}, ttl)
	hotService.rankCache.Set("us-nasdaq|yahoo", []core.HotItem{{Symbol: "MSFT"}}, ttl)
	hotService.searchCache.Set("us-sp500|volume|yahoo", []core.HotItem{{Symbol: "AAPL"}}, ttl)
	hotService.searchCache.Set("us-nasdaq|volume|yahoo", []core.HotItem{{Symbol: "MSFT"}}, ttl)

	hotService.InvalidatePool(pool.PoolIDUSSP500)

	if _, _, found := hotService.responseCache.Get("us-sp500|volume||1|20|yahoo"); found {
		t.Fatal("S&P 500 response cache survived invalidation")
	}
	if _, _, found := hotService.rankCache.Get("us-sp500|yahoo"); found {
		t.Fatal("S&P 500 rank cache survived invalidation")
	}
	if _, _, found := hotService.searchCache.Get("us-sp500|volume|yahoo"); found {
		t.Fatal("S&P 500 search cache survived invalidation")
	}
	if _, _, found := hotService.responseCache.Get("us-nasdaq|volume||1|20|yahoo"); !found {
		t.Fatal("Nasdaq response cache was invalidated with S&P 500")
	}
	if _, _, found := hotService.rankCache.Get("us-nasdaq|yahoo"); !found {
		t.Fatal("Nasdaq rank cache was invalidated with S&P 500")
	}
	if _, _, found := hotService.searchCache.Get("us-nasdaq|volume|yahoo"); !found {
		t.Fatal("Nasdaq search cache was invalidated with S&P 500")
	}
}

func newHotPoolTestRepositories(t *testing.T) (
	context.Context,
	*sql.DB,
	instrument.Repository,
	pool.Repository,
) {
	t.Helper()

	ctx := context.Background()
	db, err := sqlitestorage.Open(filepath.Join(t.TempDir(), "investgo.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return ctx, db, sqlitestorage.NewInstrumentRepository(db), sqlitestorage.NewPoolRepository(db)
}

func upsertHotTestInstrument(
	t *testing.T,
	ctx context.Context,
	repository instrument.Repository,
	value instrument.Instrument,
) instrument.Instrument {
	t.Helper()

	stored, err := repository.Upsert(ctx, value)
	if err != nil {
		t.Fatalf("upsert instrument %s: %v", value.Symbol, err)
	}
	return stored
}
