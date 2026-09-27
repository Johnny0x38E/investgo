package store

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"investgo/internal/core"
	"investgo/internal/logger"
)

type lookupStubProvider struct {
	name   string
	quotes map[string]core.Quote
	err    error
	calls  atomic.Int32
}

func (p *lookupStubProvider) Name() string { return p.name }

func (p *lookupStubProvider) Fetch(_ context.Context, items []core.WatchlistItem) (map[string]core.Quote, error) {
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	out := make(map[string]core.Quote, len(items))
	for _, item := range items {
		target, err := core.ResolveQuoteTarget(item)
		if err != nil {
			continue
		}
		quote, ok := p.quotes[target.Key]
		if !ok {
			continue
		}
		quote.Symbol = target.DisplaySymbol
		quote.Market = target.Market
		if quote.Currency == "" {
			quote.Currency = target.Currency
		}
		out[target.Key] = quote
	}
	return out, nil
}

func TestLookupSymbolPrefillsIdentityAndQuote(t *testing.T) {
	provider := &lookupStubProvider{
		name: "Sina Finance",
		quotes: map[string]core.Quote{
			"600519.SH": {Name: "贵州茅台", CurrentPrice: 1420.5, Currency: "CNY", Source: "Sina Finance"},
			"00700.HK":  {Name: "腾讯控股", CurrentPrice: 380.2, Currency: "HKD", Source: "Sina Finance"},
			"AAPL":      {Name: "Apple Inc.", CurrentPrice: 226.4, Currency: "USD", Source: "Sina Finance"},
		},
	}
	appStore := newLookupTestStore(t, provider)

	got, err := appStore.LookupSymbol(t.Context(), "600519", "CN-A")
	if err != nil {
		t.Fatalf("lookup 600519: %v", err)
	}
	if got.Symbol != "600519.SH" || got.Name != "贵州茅台" || got.Market != "CN-A" || got.Currency != "CNY" || got.CurrentPrice != 1420.5 {
		t.Fatalf("unexpected CN lookup: %+v", got)
	}

	got, err = appStore.LookupSymbol(t.Context(), "00700", "CN-A")
	if err != nil {
		t.Fatalf("lookup 00700 with CN hint: %v", err)
	}
	if got.Symbol != "00700.HK" || got.Name != "腾讯控股" || got.Market != "HK-MAIN" || got.Currency != "HKD" {
		t.Fatalf("expected HK retry after CN hint, got %+v", got)
	}

	got, err = appStore.LookupSymbol(t.Context(), "AAPL", "")
	if err != nil {
		t.Fatalf("lookup AAPL: %v", err)
	}
	if got.Symbol != "AAPL" || got.Name != "Apple Inc." || got.Market != "US-STOCK" || got.Currency != "USD" {
		t.Fatalf("unexpected US lookup: %+v", got)
	}
}

func TestLookupSymbolReturnsIdentityWhenQuoteFails(t *testing.T) {
	provider := &lookupStubProvider{
		name: "Sina Finance",
		err:  errors.New("upstream unavailable"),
	}
	appStore := newLookupTestStore(t, provider)

	got, err := appStore.LookupSymbol(t.Context(), "688981", "")
	if err != nil {
		t.Fatalf("lookup should succeed without live quote: %v", err)
	}
	if got.Symbol != "688981.SH" || got.Market != "CN-STAR" || got.Currency != "CNY" || got.Name != "688981.SH" || got.CurrentPrice != 0 {
		t.Fatalf("unexpected fallback lookup: %+v", got)
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("expected one quote attempt, got %d", provider.calls.Load())
	}
}

func TestLookupSymbolRequiresRecognizedTicker(t *testing.T) {
	appStore := newLookupTestStore(t, &lookupStubProvider{name: "Sina Finance"})

	if _, err := appStore.LookupSymbol(t.Context(), "   ", ""); err == nil {
		t.Fatal("expected empty symbol to fail")
	}
	if _, err := appStore.LookupSymbol(t.Context(), "???", ""); err == nil {
		t.Fatal("expected unrecognized symbol to fail")
	}
}

func newLookupTestStore(t *testing.T, provider *lookupStubProvider) *Store {
	t.Helper()
	appStore, err := NewStoreWithRepository(
		&memoryRepository{},
		map[string]core.QuoteProvider{
			"sina":   provider,
			"xueqiu": provider,
			"yahoo":  provider,
		},
		[]core.QuoteSourceOption{
			{ID: "sina", Name: "Sina Finance"},
			{ID: "xueqiu", Name: "Xueqiu"},
			{ID: "yahoo", Name: "Yahoo Finance"},
		},
		nil,
		logger.NewLogBook(10),
		"test",
		nil,
	)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return appStore
}
