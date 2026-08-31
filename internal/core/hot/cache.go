package hot

import (
	"strconv"
	"strings"
	"time"

	"investgo/internal/core"
)

// loadCachedItems loads the hot instrument list from cache;
// returns false if cache is missing or expired.
func (s *HotService) loadCachedItems(key string) ([]core.HotItem, bool) {
	cached, _, ok := s.searchCache.Get(key)
	if !ok {
		return nil, false
	}
	return cloneHotItems(cached), true
}

func (s *HotService) loadCachedResponse(key string) (core.HotListResponse, bool) {
	cached, expiresAt, ok := s.responseCache.Get(key)
	if !ok {
		return core.HotListResponse{}, false
	}
	response := cloneHotListResponse(cached)
	response.Cached = true
	response.CacheExpiresAt = ptrTime(expiresAt)
	return response, true
}

func (s *HotService) storeCachedResponse(key string, response core.HotListResponse, ttl time.Duration) time.Time {
	if ttl <= 0 {
		ttl = defaultHotCacheTTL
	}
	expiresAt := time.Now().Add(ttl)
	cached := cloneHotListResponse(response)
	cached.Cached = false
	cached.CacheExpiresAt = ptrTime(expiresAt)
	expiresAt = s.responseCache.Set(key, cached, ttl)

	return expiresAt
}

// hotSearchCacheKey generates the cache key for hot search based on category and sort order.
func hotSearchCacheKey(category core.HotCategory, sortBy core.HotSort, sourceID string) string {
	return string(category) + "|" + string(sortBy) + "|" + strings.TrimSpace(sourceID)
}

func hotResponseCacheKey(
	category core.HotCategory,
	sortBy core.HotSort,
	keyword string,
	page int,
	pageSize int,
	options HotListOptions,
) string {
	return strings.Join([]string{
		string(category),
		string(sortBy),
		keyword,
		strconv.Itoa(page),
		strconv.Itoa(pageSize),
		resolveHotQuoteSource(category, options),
	}, "|")
}

// cloneHotItems returns a caller-owned copy of items. The result is always a
// non-nil slice: hot responses must serialize empty results as [] instead of
// null, which would crash the frontend render path.
func cloneHotItems(items []core.HotItem) []core.HotItem {
	cloned := make([]core.HotItem, len(items))
	copy(cloned, items)
	return cloned
}

func cloneHotListResponse(response core.HotListResponse) core.HotListResponse {
	response.Items = cloneHotItems(response.Items)
	if response.CacheExpiresAt != nil {
		response.CacheExpiresAt = ptrTime(*response.CacheExpiresAt)
	}
	return response
}
