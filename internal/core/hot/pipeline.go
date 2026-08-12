package hot

import (
	"context"
	"fmt"
	"time"

	"investgo/internal/core"
)

// Pipeline overview
//
// Hot browse/search always separates two concerns:
//  1. Membership — who is on the list and in what order (ranking API or local pool).
//  2. Quote overlay — live prices from the user's configured market quote source.
//
// Ranking-capable sources: eastmoney, sina, xueqiu.
// Quote-only sources (yahoo, tencent, finnhub, ...): never produce membership;
// they only overlay prices onto a membership page.

// browseRankingCategory lists a CN/HK ranking-API category: resolve membership,
// fetch one ranked page, then overlay the user quote source when it differs.
func (s *HotService) browseRankingCategory(
	ctx context.Context,
	category core.HotCategory,
	sortBy core.HotSort,
	page, pageSize int,
	options HotListOptions,
) (core.HotListResponse, error) {
	quoteSourceID := resolveHotQuoteSource(category, options)
	membershipSourceID := resolveMembershipSource(category, quoteSourceID)
	if membershipSourceID == "" {
		return core.HotListResponse{}, fmt.Errorf("Hot category has no ranking membership source: %s", category)
	}

	membership, err := s.rankMembership(ctx, membershipSourceID, category, sortBy, page, pageSize)
	if err != nil {
		return core.HotListResponse{}, err
	}

	items := membership.Items
	if quoteSourceID != membershipSourceID {
		items, err = s.overlayQuotes(ctx, items, quoteSourceID)
		if err != nil {
			return core.HotListResponse{}, err
		}
		// Overlay can change sort-relevant fields on the current page only.
		sortHotItems(items, sortBy)
	}

	return core.HotListResponse{
		Category:    category,
		Sort:        sortBy,
		Page:        page,
		PageSize:    pageSize,
		Total:       membership.Total,
		HasMore:     page*pageSize < membership.Total,
		Items:       items,
		GeneratedAt: time.Now(),
	}, nil
}

// rankMembership fetches one ranked page from a ranking adapter.
func (s *HotService) rankMembership(
	ctx context.Context,
	sourceID string,
	category core.HotCategory,
	sortBy core.HotSort,
	page, pageSize int,
) (MembershipPage, error) {
	if s.rankMembershipFn != nil {
		return s.rankMembershipFn(ctx, sourceID, category, sortBy, page, pageSize)
	}
	response, err := s.listCategoryBySource(ctx, sourceID, category, sortBy, page, pageSize)
	if err != nil {
		return MembershipPage{}, err
	}
	return MembershipPage{Items: response.Items, Total: response.Total}, nil
}

// overlayQuotes re-fetches live quotes for the given page via the configured
// quote source ID and merges them onto membership rows.
func (s *HotService) overlayQuotes(ctx context.Context, items []core.HotItem, sourceID string) ([]core.HotItem, error) {
	if len(items) == 0 {
		return []core.HotItem{}, nil
	}

	var qp core.QuoteProvider
	if s.registry != nil {
		qp = s.registry.QuoteProvider(sourceID)
	}
	if qp == nil {
		return nil, fmt.Errorf("hot quote source is unsupported: %s", sourceID)
	}
	if hotItemsAlreadyUseSource(items, qp.Name()) {
		return cloneHotItems(items), nil
	}
	return s.applyProviderQuotes(ctx, items, qp)
}
