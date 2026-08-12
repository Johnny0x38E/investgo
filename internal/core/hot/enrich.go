package hot

import (
	"context"
	"fmt"
	"strings"

	"investgo/internal/core"
	"investgo/internal/core/provider"
)

// applyProviderQuotes fetches live quotes for the given items via qp and returns a new slice
// with price/volume/market-cap fields overwritten. Items for which the provider returns no quote
// are dropped. Returns an error if the provider returns no quotes at all.
func (s *HotService) applyProviderQuotes(ctx context.Context, items []core.HotItem, qp core.QuoteProvider) ([]core.HotItem, error) {
	if len(items) == 0 {
		return []core.HotItem{}, nil
	}

	watchItems := make([]core.WatchlistItem, 0, len(items))
	for _, item := range items {
		watchItems = append(watchItems, core.WatchlistItem{
			Symbol:   item.Symbol,
			Name:     item.Name,
			Market:   item.Market,
			Currency: item.Currency,
		})
	}

	quotes, err := qp.Fetch(ctx, watchItems)
	if err != nil {
		return nil, err
	}

	enriched := make([]core.HotItem, 0, len(items))
	for _, item := range items {
		target, err := core.ResolveQuoteTarget(core.WatchlistItem{
			Symbol:   item.Symbol,
			Name:     item.Name,
			Market:   item.Market,
			Currency: item.Currency,
		})
		if err != nil {
			continue
		}
		quote, ok := quotes[target.Key]
		if !ok {
			continue
		}

		item.Name = provider.FirstNonEmpty(quote.Name, item.Name)
		item.Currency = provider.FirstNonEmpty(quote.Currency, item.Currency)
		item.CurrentPrice = quote.CurrentPrice
		item.Change = quote.Change
		item.ChangePercent = quote.ChangePercent
		item.QuoteSource = quote.Source
		if quote.Volume > 0 {
			item.Volume = quote.Volume
		}
		if quote.MarketCap > 0 {
			item.MarketCap = quote.MarketCap
		}
		if !quote.UpdatedAt.IsZero() {
			item.UpdatedAt = quote.UpdatedAt
		}
		enriched = append(enriched, item)
	}

	if len(enriched) == 0 {
		return nil, fmt.Errorf("No live hot quotes are available from %s", qp.Name())
	}
	return enriched, nil
}

// hotItemsAlreadyUseSource reports whether every item in the slice carries quotes from the
// named source. Returns true for an empty slice (nothing to re-fetch).
func hotItemsAlreadyUseSource(items []core.HotItem, source string) bool {
	if len(items) == 0 {
		return true
	}
	source = strings.TrimSpace(source)
	for _, item := range items {
		if strings.TrimSpace(item.QuoteSource) != source {
			return false
		}
	}
	return true
}
