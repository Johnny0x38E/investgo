package hot

import (
	"investgo/internal/core"
	"investgo/internal/core/instrument"
	"investgo/internal/core/pool"
)

const BuiltInPoolDataVersion = "2026-08-31.3"

// BuiltInPoolMember is provider-neutral seed metadata used to populate the
// canonical catalog before built-in membership is stored.
type BuiltInPoolMember struct {
	Symbol   string
	Name     string
	Market   string
	Exchange string
	Currency string
}

// BuiltInPoolBaseline describes one shipped pool definition and its versioned
// baseline membership. Members is always returned as caller-owned data.
type BuiltInPoolBaseline struct {
	Pool    pool.Pool
	Members []BuiltInPoolMember
}

// BuiltInPoolBaselines exposes the shipped baseline through a read-only adapter
// instead of allowing persistence code to depend on package-global slices.
func BuiltInPoolBaselines() []BuiltInPoolBaseline {
	baselines := []BuiltInPoolBaseline{
		baselineFromHotSeeds(
			pool.Pool{ID: pool.PoolIDUSSP500, Name: "S&P 500", Market: "US-STOCK", AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex},
			normalizedUSHotSeeds(core.HotCategoryUSSP500, hotConstituents[core.HotCategoryUSSP500]),
		),
		baselineFromHotSeeds(
			pool.Pool{ID: pool.PoolIDUSNasdaq, Name: "Nasdaq 100", Market: "US-STOCK", AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex},
			normalizedUSHotSeeds(core.HotCategoryUSNasdaq, hotConstituents[core.HotCategoryUSNasdaq]),
		),
		baselineFromHotSeeds(
			pool.Pool{ID: pool.PoolIDUSDow, Name: "Dow Jones 30", Market: "US-STOCK", AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex},
			normalizedUSHotSeeds(core.HotCategoryUSDow, hotConstituents[core.HotCategoryUSDow]),
		),
		baselineFromHotSeeds(
			pool.Pool{ID: pool.PoolIDUSETF, Name: "US ETF", Market: "US-ETF", AssetClass: instrument.AssetClassETF, Type: pool.TypeBuiltIn},
			normalizedUSHotSeeds(core.HotCategoryUSETF, hotConstituents[core.HotCategoryUSETF]),
		),
		baselineFromHotSeeds(
			pool.Pool{ID: pool.PoolIDCNA, Name: "A-shares", Market: "CN-A", AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex},
			cnAConstituents,
		),
		baselineFromHotSeeds(
			pool.Pool{ID: pool.PoolIDCNETF, Name: "China ETF", Market: "CN-ETF", AssetClass: instrument.AssetClassETF, Type: pool.TypeBuiltIn},
			cnETFConstituents,
		),
		baselineFromHotSeeds(
			pool.Pool{ID: pool.PoolIDHK, Name: "Hong Kong", Market: "HK-MAIN", AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex},
			hkStockConstituents,
		),
		baselineFromHotSeeds(
			pool.Pool{ID: pool.PoolIDHKETF, Name: "Hong Kong ETF", Market: "HK-ETF", AssetClass: instrument.AssetClassETF, Type: pool.TypeBuiltIn},
			hkETFConstituents,
		),
	}
	return cloneBuiltInPoolBaselines(baselines)
}

func baselineFromHotSeeds(definition pool.Pool, seeds []hotSeed) BuiltInPoolBaseline {
	members := make([]BuiltInPoolMember, 0, len(seeds))
	for _, seed := range seeds {
		members = append(members, BuiltInPoolMember{
			Symbol:   seed.Symbol,
			Name:     seed.Name,
			Market:   seed.Market,
			Currency: seed.Currency,
		})
	}
	return BuiltInPoolBaseline{Pool: definition, Members: members}
}

func cloneBuiltInPoolBaselines(values []BuiltInPoolBaseline) []BuiltInPoolBaseline {
	cloned := make([]BuiltInPoolBaseline, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].Members = append([]BuiltInPoolMember(nil), value.Members...)
	}
	return cloned
}
