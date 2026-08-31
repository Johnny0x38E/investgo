package hot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"investgo/internal/core"
	"investgo/internal/core/marketdata"
)

type stubQuoteProvider struct {
	name  string
	calls int
	lastN int
}

func (p *stubQuoteProvider) Name() string { return p.name }

func (p *stubQuoteProvider) Fetch(_ context.Context, items []core.WatchlistItem) (map[string]core.Quote, error) {
	p.calls++
	p.lastN = len(items)
	out := make(map[string]core.Quote, len(items))
	for _, item := range items {
		target, err := core.ResolveQuoteTarget(item)
		if err != nil {
			continue
		}
		out[target.Key] = core.Quote{
			Symbol:        target.DisplaySymbol,
			Name:          item.Name + "-overlay",
			Market:        target.Market,
			Currency:      target.Currency,
			CurrentPrice:  101,
			PreviousClose: 100,
			Change:        1,
			ChangePercent: 1,
			Volume:        999,
			Source:        p.name,
			UpdatedAt:     time.Now(),
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("stub quote provider empty")
	}
	return out, nil
}

func membershipItem(symbol, name, market, currency, source string, changePct float64) core.HotItem {
	return core.HotItem{
		Symbol:        symbol,
		Name:          name,
		Market:        market,
		Currency:      currency,
		CurrentPrice:  10,
		Change:        changePct / 10,
		ChangePercent: changePct,
		Volume:        100,
		QuoteSource:   source,
		UpdatedAt:     time.Now(),
	}
}

func TestListRoutesEveryCategoryThroughThePool(t *testing.T) {
	t.Parallel()

	categories := []core.HotCategory{
		core.HotCategoryCNA,
		core.HotCategoryCNETF,
		core.HotCategoryHK,
		core.HotCategoryHKETF,
		core.HotCategoryUSSP500,
		core.HotCategoryUSNasdaq,
		core.HotCategoryUSDow,
		core.HotCategoryUSETF,
	}

	for _, category := range categories {
		t.Run(string(category), func(t *testing.T) {
			t.Parallel()

			poolHits := 0
			yahooQP := &stubQuoteProvider{name: "Yahoo Finance"}
			reg := marketdata.NewRegistry()
			reg.Register(marketdata.NewDataSource("yahoo", "Yahoo", "", nil, yahooQP, nil))

			svc := NewHotService(nil, nil, reg)
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
			if _, err := svc.List(context.Background(), category, core.HotSortVolume, "", 1, 5, opts); err != nil {
				t.Fatalf("List(%s): %v", category, err)
			}
			if poolHits == 0 {
				t.Fatalf("expected pool route for %s", category)
			}
		})
	}
}
