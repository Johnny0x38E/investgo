package hot

import (
	"context"
	"fmt"
	"testing"

	"investgo/internal/core"
	"investgo/internal/core/marketdata"
)

func TestListRoutesByCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		category core.HotCategory
		wantPool bool
	}{
		{name: "CN-A ranking", category: core.HotCategoryCNA, wantPool: false},
		{name: "CN-ETF ranking", category: core.HotCategoryCNETF, wantPool: false},
		{name: "HK ranking", category: core.HotCategoryHK, wantPool: false},
		{name: "US SP500 pool", category: core.HotCategoryUSSP500, wantPool: true},
		{name: "US Nasdaq pool", category: core.HotCategoryUSNasdaq, wantPool: true},
		{name: "US Dow pool", category: core.HotCategoryUSDow, wantPool: true},
		{name: "US ETF pool", category: core.HotCategoryUSETF, wantPool: true},
		{name: "HK ETF pool", category: core.HotCategoryHKETF, wantPool: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rankingHits := 0
			poolHits := 0

			yahooQP := &stubQuoteProvider{name: "Yahoo Finance"}
			sinaQP := &stubQuoteProvider{name: "Sina"}
			reg := marketdata.NewRegistry()
			reg.Register(marketdata.NewDataSource("yahoo", "Yahoo", "", nil, yahooQP, nil))
			reg.Register(marketdata.NewDataSource("sina", "Sina", "", nil, sinaQP, nil))

			svc := NewHotService(nil, nil, reg)
			svc.rankMembershipFn = func(
				_ context.Context,
				_ string,
				category core.HotCategory,
				_ core.HotSort,
				_ int,
				_ int,
			) (MembershipPage, error) {
				rankingHits++
				return MembershipPage{
					Total: 1,
					Items: []core.HotItem{membershipItem("600519.SH", "Moutai", "CN-A", "CNY", "Sina", 1)},
				}, nil
			}
			svc.poolQuoteFn = func(_ context.Context, seeds []hotSeed, _ string) ([]core.HotItem, error) {
				poolHits++
				if len(seeds) == 0 {
					return nil, fmt.Errorf("empty seeds")
				}
				items := make([]core.HotItem, 0, len(seeds))
				for _, seed := range seeds {
					items = append(items, membershipItem(seed.Symbol, seed.Name, seed.Market, seed.Currency, "Yahoo Finance", 1))
				}
				return items, nil
			}

			opts := HotListOptions{
				CNQuoteSource: "sina",
				HKQuoteSource: "yahoo",
				USQuoteSource: "yahoo",
				CacheTTL:      defaultHotCacheTTL,
				BypassCache:   true,
			}
			_, err := svc.List(context.Background(), tt.category, core.HotSortVolume, "", 1, 5, opts)
			if err != nil {
				t.Fatalf("List(%s): %v", tt.category, err)
			}
			if tt.wantPool {
				if poolHits == 0 || rankingHits != 0 {
					t.Fatalf("expected pool route; poolHits=%d rankingHits=%d", poolHits, rankingHits)
				}
			} else if rankingHits == 0 || poolHits != 0 {
				t.Fatalf("expected ranking route; poolHits=%d rankingHits=%d", poolHits, rankingHits)
			}
		})
	}
}
