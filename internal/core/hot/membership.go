package hot

import (
	"context"

	"investgo/internal/core"
)

// MembershipPage is one ranked page of hot-list membership (who is on the list
// and in what order). Quote fields may be present when the ranking source also
// returns prices; the pipeline may still overlay the user's configured quote source.
type MembershipPage struct {
	Items []core.HotItem
	Total int
}

// MembershipSource produces ranked hot-list membership for categories that have
// an upstream ranking API (EastMoney / Sina / Xueqiu). All shipped hot categories
// are now constituent-pool backed; this interface remains for tests and adapters.
type MembershipSource interface {
	Rank(ctx context.Context, category core.HotCategory, sortBy core.HotSort, page int, pageSize int) (MembershipPage, error)
}

// isPoolCategory reports whether the category is backed by a local constituent
// pool rather than an upstream ranking API.
func isPoolCategory(c core.HotCategory) bool {
	return isCNHotCategory(c) || isHKHotCategory(c) || isUSHotCategory(c)
}

// sourceCanRank reports whether sourceID can produce a ranked membership page
// for the given category. Only EastMoney, Sina, and Xueqiu are rank-capable;
// Yahoo and API-key providers are quote-only.
func sourceCanRank(sourceID string, category core.HotCategory) bool {
	switch sourceID {
	case "eastmoney":
		return category == core.HotCategoryCNA || category == core.HotCategoryHK
	case "sina":
		return category == core.HotCategoryCNA || category == core.HotCategoryCNETF
	case "xueqiu":
		return category == core.HotCategoryCNA ||
			category == core.HotCategoryCNETF ||
			category == core.HotCategoryHK ||
			category == core.HotCategoryHKETF
	default:
		return false
	}
}

// defaultMembershipSource returns the ranking adapter used when the user's
// configured quote source cannot rank the category. US categories have no
// ranking adapter.
func defaultMembershipSource(category core.HotCategory) string {
	switch category {
	case core.HotCategoryCNA, core.HotCategoryCNETF:
		return "sina"
	case core.HotCategoryHK, core.HotCategoryHKETF:
		return "xueqiu"
	default:
		return ""
	}
}

// resolveMembershipSource picks the ranking adapter for browseRankingCategory
// (kept for tests and unused List fallback). List() routes every shipped
// category through browsePoolCategory via isPoolCategory; US and HK-ETF never
// had a ranking browse path.
func resolveMembershipSource(category core.HotCategory, quoteSourceID string) string {
	if isUSHotCategory(category) || category == core.HotCategoryHKETF {
		return ""
	}
	if sourceCanRank(quoteSourceID, category) {
		return quoteSourceID
	}
	return defaultMembershipSource(category)
}
