package hot

import (
	"context"
	"fmt"
	"strings"

	"investgo/internal/core"
	"investgo/internal/core/instrument"
	"investgo/internal/core/pool"
)

// PoolMembership is the read side of persistent pool management consumed by
// hot-list ranking and search paths.
type PoolMembership interface {
	EffectiveMembers(ctx context.Context, poolID string) ([]instrument.Instrument, error)
	ExcludedInstrumentKeys(ctx context.Context, poolID string) ([]instrument.Identity, error)
}

func hotCategoryPoolID(category core.HotCategory) string {
	switch category {
	case core.HotCategoryCNA:
		return pool.PoolIDCNA
	case core.HotCategoryCNETF:
		return pool.PoolIDCNETF
	case core.HotCategoryHK:
		return pool.PoolIDHK
	case core.HotCategoryHKETF:
		return pool.PoolIDHKETF
	case core.HotCategoryUSSP500:
		return pool.PoolIDUSSP500
	case core.HotCategoryUSNasdaq:
		return pool.PoolIDUSNasdaq
	case core.HotCategoryUSDow:
		return pool.PoolIDUSDow
	case core.HotCategoryUSETF:
		return pool.PoolIDUSETF
	default:
		return ""
	}
}

func (s *HotService) poolSeedsForCategory(ctx context.Context, category core.HotCategory) ([]hotSeed, error) {
	if s.poolMembership == nil {
		return staticPoolSeedsForCategory(category), nil
	}
	poolID := hotCategoryPoolID(category)
	if poolID == "" {
		return staticPoolSeedsForCategory(category), nil
	}
	members, err := s.poolMembership.EffectiveMembers(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("load effective hot pool %s: %w", poolID, err)
	}
	seeds := make([]hotSeed, 0, len(members))
	for _, member := range members {
		seeds = append(seeds, instrumentHotSeed(member))
	}
	return seeds, nil
}

func (s *HotService) filterExcludedHotItems(
	ctx context.Context,
	category core.HotCategory,
	items []core.HotItem,
) ([]core.HotItem, int, error) {
	if s.poolMembership == nil {
		return items, 0, nil
	}
	poolID := hotCategoryPoolID(category)
	if poolID == "" {
		return items, 0, nil
	}
	excluded, err := s.poolMembership.ExcludedInstrumentKeys(ctx, poolID)
	if err != nil {
		return nil, 0, fmt.Errorf("load hot pool exclusions %s: %w", poolID, err)
	}
	if len(excluded) == 0 {
		return items, 0, nil
	}
	excludedSet := make(map[instrument.Identity]struct{}, len(excluded))
	for _, identity := range excluded {
		excludedSet[identity] = struct{}{}
	}
	filtered := make([]core.HotItem, 0, len(items))
	for _, item := range items {
		identity, err := hotItemIdentity(category, item)
		if err == nil {
			if _, hidden := excludedSet[identity]; hidden {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered, len(excludedSet), nil
}

func (s *HotService) filterExcludedHotSeeds(
	ctx context.Context,
	category core.HotCategory,
	seeds []hotSeed,
) ([]hotSeed, error) {
	items := make([]core.HotItem, 0, len(seeds))
	for _, seed := range seeds {
		items = append(items, core.HotItem{
			Symbol: seed.Symbol, Name: seed.Name, Market: seed.Market, Currency: seed.Currency,
		})
	}
	filtered, _, err := s.filterExcludedHotItems(ctx, category, items)
	if err != nil {
		return nil, err
	}
	result := make([]hotSeed, 0, len(filtered))
	for _, item := range filtered {
		result = append(result, hotSeed{
			Symbol: item.Symbol, Name: item.Name, Market: item.Market, Currency: item.Currency,
		})
	}
	return result, nil
}

// InvalidatePool evicts only cache entries associated with the hot category
// backed by poolID.
func (s *HotService) InvalidatePool(poolID string) {
	if s == nil {
		return
	}
	category := hotCategoryForPoolID(strings.TrimSpace(poolID))
	if category == "" {
		return
	}
	prefix := string(category) + "|"
	s.responseCache.DeleteFunc(func(key string) bool { return strings.HasPrefix(key, prefix) })
	s.rankCache.DeleteFunc(func(key string) bool { return strings.HasPrefix(key, prefix) })
	s.searchCache.DeleteFunc(func(key string) bool { return strings.HasPrefix(key, prefix) })
}

func hotCategoryForPoolID(poolID string) core.HotCategory {
	switch poolID {
	case pool.PoolIDCNA:
		return core.HotCategoryCNA
	case pool.PoolIDCNETF:
		return core.HotCategoryCNETF
	case pool.PoolIDHK:
		return core.HotCategoryHK
	case pool.PoolIDHKETF:
		return core.HotCategoryHKETF
	case pool.PoolIDUSSP500:
		return core.HotCategoryUSSP500
	case pool.PoolIDUSNasdaq:
		return core.HotCategoryUSNasdaq
	case pool.PoolIDUSDow:
		return core.HotCategoryUSDow
	case pool.PoolIDUSETF:
		return core.HotCategoryUSETF
	default:
		return ""
	}
}

func staticPoolSeedsForCategory(category core.HotCategory) []hotSeed {
	switch category {
	case core.HotCategoryCNA:
		return append([]hotSeed(nil), cnAConstituents...)
	case core.HotCategoryCNETF:
		return append([]hotSeed(nil), cnETFConstituents...)
	case core.HotCategoryHK:
		return append([]hotSeed(nil), hkStockConstituents...)
	case core.HotCategoryHKETF:
		return append([]hotSeed(nil), hkETFConstituents...)
	default:
		return normalizedUSHotSeeds(category, hotConstituents[category])
	}
}

func instrumentHotSeed(value instrument.Instrument) hotSeed {
	symbol := value.Symbol
	if strings.HasPrefix(value.Market, "HK-") && !strings.HasSuffix(symbol, ".HK") {
		symbol += ".HK"
	}
	if strings.HasPrefix(value.Market, "CN-") {
		suffix := ""
		switch value.Exchange {
		case "SSE":
			suffix = ".SH"
		case "SZSE":
			suffix = ".SZ"
		case "BSE":
			suffix = ".BJ"
		}
		if suffix != "" && !strings.HasSuffix(symbol, suffix) {
			symbol += suffix
		}
	}
	return hotSeed{
		Symbol:   symbol,
		Name:     value.Name,
		Market:   value.Market,
		Currency: value.QuoteCurrency,
	}
}

func hotItemIdentity(category core.HotCategory, item core.HotItem) (instrument.Identity, error) {
	assetClass, market := hotCategoryIdentityDefaults(category)
	if strings.TrimSpace(item.Market) != "" {
		market = item.Market
	}
	return instrument.NormalizeIdentity(instrument.Identity{
		AssetClass: assetClass,
		Market:     market,
		Symbol:     item.Symbol,
	})
}

func hotCategoryIdentityDefaults(category core.HotCategory) (instrument.AssetClass, string) {
	switch category {
	case core.HotCategoryCNETF:
		return instrument.AssetClassETF, "CN-ETF"
	case core.HotCategoryHKETF:
		return instrument.AssetClassETF, "HK-ETF"
	case core.HotCategoryUSETF:
		return instrument.AssetClassETF, "US-ETF"
	case core.HotCategoryHK:
		return instrument.AssetClassEquity, "HK-MAIN"
	case core.HotCategoryUSSP500, core.HotCategoryUSNasdaq, core.HotCategoryUSDow:
		return instrument.AssetClassEquity, "US-STOCK"
	default:
		return instrument.AssetClassEquity, "CN-A"
	}
}
