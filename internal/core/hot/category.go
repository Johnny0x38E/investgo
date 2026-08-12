package hot

import (
	"strings"

	"investgo/internal/core"
)

// isCNHotCategory checks whether the category belongs to the A-share market (including ETFs).
func isCNHotCategory(c core.HotCategory) bool {
	return c == core.HotCategoryCNA || c == core.HotCategoryCNETF
}

// isHKHotCategory checks whether the category belongs to the Hong Kong stock market.
func isHKHotCategory(c core.HotCategory) bool {
	return c == core.HotCategoryHK || c == core.HotCategoryHKETF
}

// isUSHotCategory checks whether the category belongs to the US stock market.
func isUSHotCategory(c core.HotCategory) bool {
	switch c {
	case core.HotCategoryUSSP500, core.HotCategoryUSNasdaq, core.HotCategoryUSDow, core.HotCategoryUSETF:
		return true
	}
	return false
}

func normaliseHotListOptions(options HotListOptions) HotListOptions {
	options.CNQuoteSource = normaliseHotQuoteSourceID(options.CNQuoteSource)
	options.HKQuoteSource = normaliseHotQuoteSourceID(options.HKQuoteSource)
	options.USQuoteSource = normaliseHotQuoteSourceID(options.USQuoteSource)
	if options.CacheTTL <= 0 {
		options.CacheTTL = defaultHotCacheTTL
	}
	return options
}

func normaliseHotQuoteSourceID(sourceID string) string {
	switch strings.ToLower(strings.TrimSpace(sourceID)) {
	case "yahoo":
		return "yahoo"
	case "alpha-vantage":
		return "alpha-vantage"
	case "twelve-data":
		return "twelve-data"
	case "finnhub":
		return "finnhub"
	case "tiingo":
		return "tiingo"
	case "polygon":
		return "polygon"
	case "sina":
		return "sina"
	case "xueqiu":
		return "xueqiu"
	case "tencent":
		return "tencent"
	default:
		return "eastmoney"
	}
}

func resolveHotQuoteSource(category core.HotCategory, options HotListOptions) string {
	if isCNHotCategory(category) {
		return options.CNQuoteSource
	}
	if isHKHotCategory(category) {
		return options.HKQuoteSource
	}
	if isUSHotCategory(category) {
		return options.USQuoteSource
	}
	return "eastmoney"
}

// effectivePoolQuoteSource applies per-category overrides for pool quote sources.
// For HK ETF, EastMoney push2 is unreliable; fall back to Tencent when the
// configured source is the eastmoney default and the user hasn't changed it.
// For US pools, API-key sources with tight per-symbol quotas cannot quote a
// full index/ETF constituent set, so fall back to Sina's batch US quotes.
func effectivePoolQuoteSource(category core.HotCategory, sourceID string) string {
	if category == core.HotCategoryHKETF && sourceID == "eastmoney" {
		return "tencent"
	}
	if isUSHotCategory(category) && isConstrainedUSPoolSource(sourceID) {
		return "sina"
	}
	return sourceID
}

// isConstrainedUSPoolSource reports quote sources that are unsuitable for
// full-pool US hot lists (hundreds of symbols per refresh).
func isConstrainedUSPoolSource(sourceID string) bool {
	switch strings.ToLower(strings.TrimSpace(sourceID)) {
	case "tiingo", "finnhub", "alpha-vantage", "twelve-data", "polygon":
		return true
	default:
		return false
	}
}

// normaliseHotCategory falls back missing or invalid categories to the default value.
func normaliseHotCategory(c core.HotCategory) core.HotCategory {
	switch c {
	case core.HotCategoryCNA, core.HotCategoryCNETF,
		core.HotCategoryHK, core.HotCategoryHKETF,
		core.HotCategoryUSSP500, core.HotCategoryUSNasdaq, core.HotCategoryUSDow, core.HotCategoryUSETF:
		return c
	}
	return core.HotCategoryCNA
}

// normaliseHotSort falls back missing or invalid sort fields to the default value.
func normaliseHotSort(s core.HotSort) core.HotSort {
	switch s {
	case core.HotSortVolume, core.HotSortGainers, core.HotSortLosers, core.HotSortMarketCap, core.HotSortPrice:
		return s
	}
	return core.HotSortVolume
}

func normaliseHotKeyword(keyword string) string {
	return strings.ToLower(strings.TrimSpace(keyword))
}
