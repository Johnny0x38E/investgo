package hot

import (
	"context"
	"testing"
	"time"

	"investgo/internal/core"
)

func TestPoolRankTTL(t *testing.T) {
	t.Parallel()

	if got := poolRankTTL(60 * time.Second); got != minPoolRankTTL {
		t.Fatalf("poolRankTTL(60s) = %v; want %v", got, minPoolRankTTL)
	}
	if got := poolRankTTL(10 * time.Minute); got != 10*time.Minute {
		t.Fatalf("poolRankTTL(10m) = %v; want 10m", got)
	}
}

func TestGetOrFetchPoolRankCaches(t *testing.T) {
	t.Parallel()

	svc := NewHotService(nil, nil, nil)
	fetches := 0
	svc.poolQuoteFn = func(_ context.Context, seeds []hotSeed, sourceID string) ([]core.HotItem, error) {
		fetches++
		if sourceID != "yahoo" {
			t.Fatalf("sourceID=%s; want yahoo", sourceID)
		}
		items := make([]core.HotItem, 0, len(seeds))
		for i, seed := range seeds {
			items = append(items, core.HotItem{
				Symbol:        seed.Symbol,
				Name:          seed.Name,
				Market:        seed.Market,
				Currency:      seed.Currency,
				CurrentPrice:  float64(100 + i),
				ChangePercent: float64(i),
				Volume:        float64(1000 - i),
				QuoteSource:   "Yahoo Finance",
				UpdatedAt:     time.Now(),
			})
		}
		return items, nil
	}

	// Use Dow (small pool) to keep the stub cheap.
	first, fromCache, err := svc.getOrFetchPoolRank(
		context.Background(),
		core.HotCategoryUSDow,
		"yahoo",
		time.Minute,
		false,
	)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if fromCache || fetches != 1 {
		t.Fatalf("first fetch fromCache=%v fetches=%d", fromCache, fetches)
	}
	if len(first) == 0 {
		t.Fatal("expected dow constituents")
	}

	second, fromCache, err := svc.getOrFetchPoolRank(
		context.Background(),
		core.HotCategoryUSDow,
		"yahoo",
		time.Minute,
		false,
	)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if !fromCache || fetches != 1 {
		t.Fatalf("second fetch should hit cache; fromCache=%v fetches=%d", fromCache, fetches)
	}
	if len(second) != len(first) {
		t.Fatalf("cached len=%d; want %d", len(second), len(first))
	}

	_, fromCache, err = svc.getOrFetchPoolRank(
		context.Background(),
		core.HotCategoryUSDow,
		"yahoo",
		time.Minute,
		true,
	)
	if err != nil {
		t.Fatalf("bypass fetch: %v", err)
	}
	if fromCache || fetches != 2 {
		t.Fatalf("bypass should refetch; fromCache=%v fetches=%d", fromCache, fetches)
	}
}

func TestBrowsePoolCategoryPageOverlayOnCacheHit(t *testing.T) {
	t.Parallel()

	svc := NewHotService(nil, nil, nil)
	fullFetches := 0
	pageFetches := 0

	svc.poolQuoteFn = func(_ context.Context, seeds []hotSeed, _ string) ([]core.HotItem, error) {
		if len(seeds) <= 5 {
			pageFetches++
		} else {
			fullFetches++
		}
		items := make([]core.HotItem, 0, len(seeds))
		for i, seed := range seeds {
			items = append(items, core.HotItem{
				Symbol:        seed.Symbol,
				Name:          seed.Name,
				Market:        seed.Market,
				Currency:      seed.Currency,
				CurrentPrice:  float64(50 + i),
				ChangePercent: float64(len(seeds) - i), // first seed highest gainer on full fetch
				Volume:        float64(i + 1),
				QuoteSource:   "Yahoo Finance",
				UpdatedAt:     time.Now(),
			})
		}
		return items, nil
	}

	opts := HotListOptions{USQuoteSource: "yahoo", CacheTTL: time.Minute}

	first, err := svc.browsePoolCategory(
		context.Background(),
		core.HotCategoryUSDow,
		core.HotSortGainers,
		1,
		5,
		opts,
	)
	if err != nil {
		t.Fatalf("first browse: %v", err)
	}
	if fullFetches != 1 || pageFetches != 0 {
		t.Fatalf("cold browse full=%d page=%d; want full=1 page=0", fullFetches, pageFetches)
	}
	if len(first.Items) != 5 {
		t.Fatalf("page size=%d; want 5", len(first.Items))
	}

	second, err := svc.browsePoolCategory(
		context.Background(),
		core.HotCategoryUSDow,
		core.HotSortGainers,
		1,
		5,
		opts,
	)
	if err != nil {
		t.Fatalf("second browse: %v", err)
	}
	if fullFetches != 1 {
		t.Fatalf("warm browse should reuse rank cache; full=%d", fullFetches)
	}
	if pageFetches != 1 {
		t.Fatalf("warm browse should refresh page only; page=%d", pageFetches)
	}
	if len(second.Items) != 5 {
		t.Fatalf("second page size=%d; want 5", len(second.Items))
	}
}

func TestEffectivePoolQuoteSourceHKETF(t *testing.T) {
	t.Parallel()

	if got := effectivePoolQuoteSource(core.HotCategoryHKETF, "eastmoney"); got != "tencent" {
		t.Fatalf("HKETF eastmoney override = %q; want tencent", got)
	}
	if got := effectivePoolQuoteSource(core.HotCategoryHKETF, "yahoo"); got != "yahoo" {
		t.Fatalf("HKETF yahoo = %q; want yahoo", got)
	}
}

func TestEffectivePoolQuoteSourceUSConstrainedAPI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source string
		want   string
	}{
		{source: "tiingo", want: "sina"},
		{source: "finnhub", want: "sina"},
		{source: "alpha-vantage", want: "sina"},
		{source: "twelve-data", want: "sina"},
		{source: "polygon", want: "sina"},
		{source: "yahoo", want: "yahoo"},
		{source: "tencent", want: "tencent"},
		{source: "sina", want: "sina"},
		{source: "eastmoney", want: "eastmoney"},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			if got := effectivePoolQuoteSource(core.HotCategoryUSSP500, tc.source); got != tc.want {
				t.Fatalf("US pool source %q = %q; want %q", tc.source, got, tc.want)
			}
			if got := effectivePoolQuoteSource(core.HotCategoryUSETF, tc.source); got != tc.want {
				t.Fatalf("US ETF pool source %q = %q; want %q", tc.source, got, tc.want)
			}
		})
	}
}
