package hot

import (
	"context"
	"fmt"
	"time"

	"investgo/internal/core"
)

const minPoolRankTTL = 5 * time.Minute

func poolRankTTL(pageTTL time.Duration) time.Duration {
	return max(pageTTL, minPoolRankTTL)
}

func poolRankCacheKey(category core.HotCategory, sourceID string) string {
	return string(category) + "|" + sourceID
}

// getOrFetchPoolRank returns full-pool quotes used for global sorting.
// On cache hit, fromCache is true and callers should refresh the current page
// for display freshness. On miss, the returned quotes are already fresh.
func (s *HotService) getOrFetchPoolRank(
	ctx context.Context,
	category core.HotCategory,
	sourceID string,
	ttl time.Duration,
	bypass bool,
) (items []core.HotItem, fromCache bool, err error) {
	key := poolRankCacheKey(category, sourceID)
	if !bypass && s.rankCache != nil {
		if cached, _, ok := s.rankCache.Get(key); ok {
			return cloneHotItems(cached), true, nil
		}
	}

	items, err = s.fetchFullPoolQuotes(ctx, category, sourceID)
	if err != nil {
		return nil, false, err
	}
	if s.rankCache != nil {
		if ttl <= 0 {
			ttl = minPoolRankTTL
		}
		s.rankCache.Set(key, cloneHotItems(items), ttl)
	}
	return items, false, nil
}

func (s *HotService) fetchFullPoolQuotes(ctx context.Context, category core.HotCategory, sourceID string) ([]core.HotItem, error) {
	pool := poolSeedsForCategory(category)
	if len(pool) == 0 {
		return nil, fmt.Errorf("No available hot pool for category: %s", category)
	}
	if s.poolQuoteFn != nil {
		return s.poolQuoteFn(ctx, pool, sourceID)
	}
	return s.loadHotItemsForSeeds(ctx, pool, HotListOptions{
		CNQuoteSource: sourceID,
		HKQuoteSource: sourceID,
		USQuoteSource: sourceID,
	})
}

func poolSeedsForCategory(category core.HotCategory) []hotSeed {
	if category == core.HotCategoryHKETF {
		return hkETFConstituents
	}
	return normalizedUSHotSeeds(category, hotConstituents[category])
}

// browsePoolCategory lists a pool-backed category using two-tier caching:
// long-TTL full-pool quotes for global sort, then optional page quote refresh.
func (s *HotService) browsePoolCategory(
	ctx context.Context,
	category core.HotCategory,
	sortBy core.HotSort,
	page, pageSize int,
	options HotListOptions,
) (core.HotListResponse, error) {
	quoteSourceID := effectivePoolQuoteSource(category, resolveHotQuoteSource(category, options))
	ranked, fromCache, err := s.getOrFetchPoolRank(
		ctx,
		category,
		quoteSourceID,
		poolRankTTL(options.CacheTTL),
		options.BypassCache,
	)
	if err != nil {
		return core.HotListResponse{}, err
	}

	sortHotItems(ranked, sortBy)
	start, end := paginateHotItems(len(ranked), page, pageSize)
	pageItems := cloneHotItems(ranked[start:end])

	// Rank-cache hits keep sort order from longer-TTL data; refresh only the
	// visible page so display prices follow the short page TTL semantics.
	if fromCache {
		pageItems, err = s.forceOverlayQuotes(ctx, pageItems, quoteSourceID)
		if err != nil {
			return core.HotListResponse{}, err
		}
	}

	return core.HotListResponse{
		Category:    category,
		Sort:        sortBy,
		Page:        page,
		PageSize:    pageSize,
		Total:       len(ranked),
		HasMore:     end < len(ranked),
		Items:       pageItems,
		GeneratedAt: time.Now(),
	}, nil
}

// forceOverlayQuotes always re-fetches quotes for the page, ignoring matching
// QuoteSource labels from a warm rank cache.
func (s *HotService) forceOverlayQuotes(ctx context.Context, items []core.HotItem, sourceID string) ([]core.HotItem, error) {
	if len(items) == 0 {
		return []core.HotItem{}, nil
	}
	var qp core.QuoteProvider
	if s.registry != nil {
		qp = s.registry.QuoteProvider(sourceID)
	}
	if qp == nil {
		// Pool path may use dedicated fetchers (yahoo/eastmoney/sina) that are
		// also reachable via fetchPoolQuotes without a registry entry in tests.
		seeds := make([]hotSeed, 0, len(items))
		for _, item := range items {
			seeds = append(seeds, hotSeed{
				Symbol:   item.Symbol,
				Name:     item.Name,
				Market:   item.Market,
				Currency: item.Currency,
			})
		}
		if s.poolQuoteFn != nil {
			return s.poolQuoteFn(ctx, seeds, sourceID)
		}
		return s.fetchPoolQuotes(ctx, seeds, sourceID)
	}
	return s.applyProviderQuotes(ctx, items, qp)
}
