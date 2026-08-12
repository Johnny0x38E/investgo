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

func TestBrowseRankingCategorySameSourceNoOverlay(t *testing.T) {
	t.Parallel()

	sinaQP := &stubQuoteProvider{name: "Sina"}
	reg := marketdata.NewRegistry()
	reg.Register(marketdata.NewDataSource("sina", "Sina", "", []string{"CN-A"}, sinaQP, nil))

	svc := NewHotService(nil, nil, reg)
	svc.rankMembershipFn = func(
		_ context.Context,
		sourceID string,
		category core.HotCategory,
		_ core.HotSort,
		_ int,
		_ int,
	) (MembershipPage, error) {
		if sourceID != "sina" || category != core.HotCategoryCNA {
			t.Fatalf("unexpected rank call: %s %s", sourceID, category)
		}
		return MembershipPage{
			Total: 1,
			Items: []core.HotItem{membershipItem("600519.SH", "Kweichow Moutai", "CN-A", "CNY", "Sina", 2)},
		}, nil
	}

	resp, err := svc.browseRankingCategory(
		context.Background(),
		core.HotCategoryCNA,
		core.HotSortVolume,
		1,
		20,
		HotListOptions{CNQuoteSource: "sina"},
	)
	if err != nil {
		t.Fatalf("browseRankingCategory: %v", err)
	}
	if sinaQP.calls != 0 {
		t.Fatalf("same-source browse should not overlay; calls=%d", sinaQP.calls)
	}
	if len(resp.Items) != 1 || resp.Items[0].QuoteSource != "Sina" {
		t.Fatalf("unexpected response: %+v", resp.Items)
	}
}

func TestBrowseRankingCategoryDifferentSourceOverlays(t *testing.T) {
	t.Parallel()

	tencentQP := &stubQuoteProvider{name: "Tencent Finance"}
	reg := marketdata.NewRegistry()
	reg.Register(marketdata.NewDataSource("tencent", "Tencent", "", []string{"CN-A"}, tencentQP, nil))

	svc := NewHotService(nil, nil, reg)
	svc.rankMembershipFn = func(
		_ context.Context,
		sourceID string,
		_ core.HotCategory,
		_ core.HotSort,
		_ int,
		_ int,
	) (MembershipPage, error) {
		if sourceID != "sina" {
			t.Fatalf("expected sina membership for tencent quote source, got %s", sourceID)
		}
		return MembershipPage{
			Total: 2,
			Items: []core.HotItem{
				membershipItem("600519.SH", "Moutai", "CN-A", "CNY", "Sina", 1),
				membershipItem("000001.SZ", "PAB", "CN-A", "CNY", "Sina", 2),
			},
		}, nil
	}

	resp, err := svc.browseRankingCategory(
		context.Background(),
		core.HotCategoryCNA,
		core.HotSortVolume,
		1,
		20,
		HotListOptions{CNQuoteSource: "tencent"},
	)
	if err != nil {
		t.Fatalf("browseRankingCategory: %v", err)
	}
	if tencentQP.calls != 1 || tencentQP.lastN != 2 {
		t.Fatalf("overlay calls=%d lastN=%d; want 1 call with 2 items", tencentQP.calls, tencentQP.lastN)
	}
	if len(resp.Items) != 2 || resp.Items[0].QuoteSource != "Tencent Finance" {
		t.Fatalf("unexpected overlay result: %+v", resp.Items)
	}
	if resp.Items[0].CurrentPrice != 101 {
		t.Fatalf("overlay price not applied: %+v", resp.Items[0])
	}
}

func TestBrowseRankingCategoryYahooQuoteSourceNoHardFail(t *testing.T) {
	t.Parallel()

	yahooQP := &stubQuoteProvider{name: "Yahoo Finance"}
	reg := marketdata.NewRegistry()
	reg.Register(marketdata.NewDataSource("yahoo", "Yahoo", "", []string{"CN-A"}, yahooQP, nil))

	svc := NewHotService(nil, nil, reg)
	svc.rankMembershipFn = func(
		_ context.Context,
		sourceID string,
		category core.HotCategory,
		_ core.HotSort,
		_ int,
		_ int,
	) (MembershipPage, error) {
		if sourceID != "sina" {
			return MembershipPage{}, fmt.Errorf("yahoo must fall back to sina membership, got %s", sourceID)
		}
		if category != core.HotCategoryCNA {
			return MembershipPage{}, fmt.Errorf("unexpected category %s", category)
		}
		return MembershipPage{
			Total: 1,
			Items: []core.HotItem{membershipItem("600519.SH", "Moutai", "CN-A", "CNY", "Sina", 1)},
		}, nil
	}

	resp, err := svc.browseRankingCategory(
		context.Background(),
		core.HotCategoryCNA,
		core.HotSortGainers,
		1,
		20,
		HotListOptions{CNQuoteSource: "yahoo"},
	)
	if err != nil {
		t.Fatalf("yahoo quote source should overlay, not hard-fail: %v", err)
	}
	if yahooQP.calls != 1 {
		t.Fatalf("expected yahoo overlay call, got %d", yahooQP.calls)
	}
	if len(resp.Items) != 1 || resp.Items[0].QuoteSource != "Yahoo Finance" {
		t.Fatalf("unexpected response: %+v", resp.Items)
	}
}
