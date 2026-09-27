package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"investgo/internal/core"
)

const symbolLookupTimeout = 8 * time.Second

// LookupSymbol resolves a typed ticker into canonical identity fields and, when a
// live quote source is available, the official name and latest price.
func (s *Store) LookupSymbol(ctx context.Context, symbol, marketHint string) (core.SymbolLookup, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return core.SymbolLookup{}, errors.New("Symbol is required")
	}

	target, err := resolveLookupTarget(symbol, marketHint)
	if err != nil {
		return core.SymbolLookup{}, err
	}

	result := core.SymbolLookup{
		Symbol:   target.DisplaySymbol,
		Name:     target.DisplaySymbol,
		Market:   target.Market,
		Currency: target.Currency,
	}

	item := core.WatchlistItem{
		Symbol:   target.DisplaySymbol,
		Name:     target.DisplaySymbol,
		Market:   target.Market,
		Currency: target.Currency,
	}

	s.mu.RLock()
	provider := s.activeQuoteProviderLocked(target.Market)
	s.mu.RUnlock()
	if provider == nil {
		return result, nil
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, symbolLookupTimeout)
		defer cancel()
	}

	quotes, fetchErr := provider.Fetch(ctx, []core.WatchlistItem{item})
	if fetchErr != nil {
		s.logWarn("lookup", fmt.Sprintf("quote lookup failed for %s: %v", target.DisplaySymbol, fetchErr))
		return result, nil
	}

	quote, ok := quotes[target.Key]
	if !ok {
		return result, nil
	}
	if name := strings.TrimSpace(quote.Name); name != "" {
		result.Name = name
	}
	result.CurrentPrice = quote.CurrentPrice
	result.QuoteSource = quote.Source
	if currency := strings.TrimSpace(quote.Currency); currency != "" {
		result.Currency = currency
	}
	return result, nil
}

func resolveLookupTarget(symbol, marketHint string) (core.QuoteTarget, error) {
	target, err := core.ResolveQuoteTarget(core.WatchlistItem{
		Symbol: symbol,
		Market: marketHint,
	})
	if err != nil && strings.TrimSpace(marketHint) != "" {
		return core.ResolveQuoteTarget(core.WatchlistItem{Symbol: symbol})
	}
	return target, err
}
