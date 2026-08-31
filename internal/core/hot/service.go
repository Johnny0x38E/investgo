package hot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	ttlcache "investgo/internal/common/cache"
	"investgo/internal/core"
	"investgo/internal/core/marketdata"
)

const (
	hotDefaultPageSize = 20
	defaultHotCacheTTL = 60 * time.Second
)

// HotListOptions carries request-scoped settings that affect hot list quote fetching.
type HotListOptions struct {
	CNQuoteSource string
	HKQuoteSource string
	USQuoteSource string
	CacheTTL      time.Duration
	BypassCache   bool
}

// HotService fetches and paginates hot lists from constituent pools.
// Live prices follow the configured market quote source.
type HotService struct {
	client         *http.Client
	log            *slog.Logger
	registry       *marketdata.Registry
	searchCache    *ttlcache.TTL[string, []core.HotItem]
	responseCache  *ttlcache.TTL[string, core.HotListResponse]
	rankCache      *ttlcache.TTL[string, []core.HotItem]
	poolMembership PoolMembership

	// poolQuoteFn overrides full-pool quote fetching in tests.
	poolQuoteFn func(ctx context.Context, seeds []hotSeed, sourceID string) ([]core.HotItem, error)
}

// NewHotService creates a hot list service.
func NewHotService(
	client *http.Client,
	logger *slog.Logger,
	registry *marketdata.Registry,
	poolMembership ...PoolMembership,
) *HotService {
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	service := &HotService{
		client:        client,
		log:           logger,
		registry:      registry,
		searchCache:   ttlcache.NewTTL[string, []core.HotItem](),
		responseCache: ttlcache.NewTTL[string, core.HotListResponse](),
		rankCache:     ttlcache.NewTTL[string, []core.HotItem](),
	}
	if len(poolMembership) > 0 {
		service.poolMembership = poolMembership[0]
	}
	return service
}

// List returns the hot list for the given category and sort order.
// Browse uses the constituent pool; keyword search uses dedicated adapters.
func (s *HotService) List(
	ctx context.Context,
	category core.HotCategory,
	sortBy core.HotSort,
	keyword string,
	page, pageSize int,
	options HotListOptions,
) (core.HotListResponse, error) {
	category = normaliseHotCategory(category)
	sortBy = normaliseHotSort(sortBy)
	keyword = normaliseHotKeyword(keyword)
	options = normaliseHotListOptions(options)
	page = max(page, 1)
	if pageSize <= 0 {
		pageSize = hotDefaultPageSize
	}

	cacheKey := hotResponseCacheKey(category, sortBy, keyword, page, pageSize, options)
	if !options.BypassCache {
		if response, ok := s.loadCachedResponse(cacheKey); ok {
			return response, nil
		}
	}

	var response core.HotListResponse
	var err error
	if keyword != "" {
		// Search fans out to external suggest + quote APIs with no cache for some
		// branches, so a single slow upstream can hold the request open for the
		// full frontend 15s timeout. Cap the whole search phase below that so
		// the user gets a fast error (or partial result) instead of a stuck UI.
		searchCtx, cancel := context.WithTimeout(ctx, hotSearchTimeout)
		defer cancel()
		response, err = s.search(searchCtx, category, sortBy, keyword, page, pageSize, options)
	} else {
		response, err = s.browsePoolCategory(ctx, category, sortBy, page, pageSize, options)
	}
	if err != nil {
		return core.HotListResponse{}, err
	}

	response.Cached = false
	expiresAt := s.storeCachedResponse(cacheKey, response, options.CacheTTL)
	response.CacheExpiresAt = ptrTime(expiresAt)
	return response, nil
}

// search filters the data pool by keyword. Each market uses a lightweight search approach:
// CN/HK uses EastMoney suggest API, US equities use local seed filtering + Yahoo search,
// US ETFs combine local pool + Yahoo search.
func (s *HotService) search(
	ctx context.Context,
	category core.HotCategory,
	sortBy core.HotSort,
	keyword string,
	page, pageSize int,
	options HotListOptions,
) (core.HotListResponse, error) {
	if category == core.HotCategoryUSETF {
		return s.searchUSETFs(ctx, sortBy, keyword, page, pageSize, options)
	}

	// Pool-backed categories (US equities) filter seeds locally first, then use
	// Yahoo search for broader coverage (e.g. name search beyond local seed names).
	if isUSHotCategory(category) {
		return s.searchUSStocks(ctx, category, sortBy, keyword, page, pageSize, options)
	}

	// CN/HK categories use EastMoney suggest API for fast keyword search.
	if isCNHotCategory(category) || isHKHotCategory(category) {
		return s.searchCNHK(ctx, category, sortBy, keyword, page, pageSize, options)
	}

	return core.HotListResponse{}, fmt.Errorf("Hot search is unsupported for category: %s", category)
}

// hotSearchMaxSeeds caps how many seeds a single hot search will load quotes for.
// A broad keyword can match dozens of Yahoo results; each result fans out to a
// real-time quote request, so an unbounded list turns one search into dozens of
// slow upstream calls. Capping the seeds keeps searches responsive and still
// returns plenty of results for the paginated UI.
const hotSearchMaxSeeds = 25

// hotSearchTimeout bounds the entire hot search phase (suggest API + quote fan-
// out). It is intentionally shorter than the frontend 15s request timeout so a
// slow upstream produces a fast, visible error rather than a frozen UI.
const hotSearchTimeout = 8 * time.Second

// searchUSETFs handles US ETF search specially:
// filter from the pool first, then call the Yahoo Finance search API for more matches,
// merge and deduplicate, and fetch real-time quotes.
func (s *HotService) searchUSETFs(
	ctx context.Context,
	sortBy core.HotSort,
	keyword string,
	page, pageSize int,
	options HotListOptions,
) (core.HotListResponse, error) {
	poolSeeds, err := s.poolSeedsForCategory(ctx, core.HotCategoryUSETF)
	if err != nil {
		return core.HotListResponse{}, err
	}
	seeds := filterHotSeeds(poolSeeds, keyword)

	remoteSeeds, err := s.searchYahooUSSeeds(ctx, keyword)
	if err == nil {
		seeds = mergeHotSeeds(seeds, remoteSeeds)
	}
	seeds, err = s.filterExcludedHotSeeds(ctx, core.HotCategoryUSETF, seeds)
	if err != nil {
		return core.HotListResponse{}, err
	}

	seeds = seeds[:min(len(seeds), hotSearchMaxSeeds)]

	items, err := s.loadHotItemsForSeeds(ctx, seeds, options)
	if err != nil {
		return core.HotListResponse{}, err
	}

	sortHotItems(items, sortBy)
	start, end := paginateHotItems(len(items), page, pageSize)
	return core.HotListResponse{
		Category:    core.HotCategoryUSETF,
		Sort:        sortBy,
		Page:        page,
		PageSize:    pageSize,
		Total:       len(items),
		HasMore:     end < len(items),
		Items:       items[start:end],
		GeneratedAt: time.Now(),
	}, nil
}

// searchUSStocks handles keyword search for US equity categories.
// It first filters the local seed pool by name/symbol, then calls Yahoo search for
// broader coverage (e.g. matching by company name that may not be in the local seed names),
// merges and deduplicates, then fetches quotes for the combined matches.
func (s *HotService) searchUSStocks(
	ctx context.Context,
	category core.HotCategory,
	sortBy core.HotSort,
	keyword string,
	page, pageSize int,
	options HotListOptions,
) (core.HotListResponse, error) {
	poolSeeds, err := s.poolSeedsForCategory(ctx, category)
	if err != nil {
		return core.HotListResponse{}, err
	}

	// Filter seeds locally — no network I/O.
	seeds := filterHotSeeds(poolSeeds, keyword)

	// Call Yahoo search for broader coverage (e.g. name-based search).
	remoteSeeds, err := s.searchYahooUSStockSeeds(ctx, keyword)
	if err == nil && len(remoteSeeds) > 0 {
		seeds = mergeHotSeeds(seeds, remoteSeeds)
	}
	seeds, err = s.filterExcludedHotSeeds(ctx, category, seeds)
	if err != nil {
		return core.HotListResponse{}, err
	}

	// Trim the merged seed list before quoting: a broad keyword can match far
	// more symbols than we have quote-budget for, and pagination handles the
	// overflow on the UI side anyway.
	seeds = seeds[:min(len(seeds), hotSearchMaxSeeds)]

	if len(seeds) == 0 {
		return core.HotListResponse{
			Category:    category,
			Sort:        sortBy,
			Page:        page,
			PageSize:    pageSize,
			Total:       0,
			HasMore:     false,
			Items:       []core.HotItem{},
			GeneratedAt: time.Now(),
		}, nil
	}

	// Only fetch quotes for the (small) set of matching seeds.
	items, err := s.loadHotItemsForSeeds(ctx, seeds, options)
	if err != nil {
		return core.HotListResponse{}, err
	}

	sortHotItems(items, sortBy)
	start, end := paginateHotItems(len(items), page, pageSize)
	return core.HotListResponse{
		Category:    category,
		Sort:        sortBy,
		Page:        page,
		PageSize:    pageSize,
		Total:       len(items),
		HasMore:     end < len(items),
		Items:       items[start:end],
		GeneratedAt: time.Now(),
	}, nil
}

// searchCNHK handles keyword search for CN and HK categories using the EastMoney suggest API.
// This replaces the old fetch-all-then-filter approach that would download thousands of items.
func (s *HotService) searchCNHK(
	ctx context.Context,
	category core.HotCategory,
	sortBy core.HotSort,
	keyword string,
	page, pageSize int,
	options HotListOptions,
) (core.HotListResponse, error) {
	// Call EastMoney suggest API — single lightweight request, returns only matches.
	seeds := s.searchEastMoneySeeds(ctx, keyword, category)

	// Also try to filter from cached items (from previous normal browsing).
	if cachedItems, ok := s.loadCachedItems(hotSearchCacheKey(category, sortBy, resolveHotQuoteSource(category, options))); ok {
		cachedMatches := filterHotItems(cachedItems, keyword)
		for _, item := range cachedMatches {
			seeds = mergeHotSeeds(seeds, []hotSeed{{
				Symbol:   item.Symbol,
				Name:     item.Name,
				Market:   item.Market,
				Currency: item.Currency,
			}})
		}
	}

	seeds, err := s.filterExcludedHotSeeds(ctx, category, seeds)
	if err != nil {
		return core.HotListResponse{}, err
	}

	// Cap the seed list before quote fetch so a broad keyword does not turn
	// into an unbounded fan-out of quote requests.
	seeds = seeds[:min(len(seeds), hotSearchMaxSeeds)]

	if len(seeds) == 0 {
		return core.HotListResponse{
			Category:    category,
			Sort:        sortBy,
			Page:        page,
			PageSize:    pageSize,
			Total:       0,
			HasMore:     false,
			Items:       []core.HotItem{},
			GeneratedAt: time.Now(),
		}, nil
	}

	// Fetch quotes only for the small set of matching seeds.
	items, err := s.loadHotItemsForSeeds(ctx, seeds, options)
	if err != nil {
		return core.HotListResponse{}, err
	}

	sortHotItems(items, sortBy)
	start, end := paginateHotItems(len(items), page, pageSize)
	return core.HotListResponse{
		Category:    category,
		Sort:        sortBy,
		Page:        page,
		PageSize:    pageSize,
		Total:       len(items),
		HasMore:     end < len(items),
		Items:       items[start:end],
		GeneratedAt: time.Now(),
	}, nil
}

// loadHotItemsForSeeds fetches real-time quotes for the given hotSeed list and returns only rows backed by live data.
func (s *HotService) loadHotItemsForSeeds(ctx context.Context, seeds []hotSeed, options HotListOptions) ([]core.HotItem, error) {
	if len(seeds) == 0 {
		return []core.HotItem{}, nil
	}

	category := categoryForHotSeeds(seeds)
	sourceID := effectivePoolQuoteSource(category, resolveHotQuoteSource(category, options))
	return s.fetchPoolQuotes(ctx, seeds, sourceID)
}

// categoryForHotSeeds infers the HotCategory from the market field of the first seed.
func categoryForHotSeeds(seeds []hotSeed) core.HotCategory {
	if len(seeds) == 0 {
		return core.HotCategoryCNA
	}
	switch seeds[0].Market {
	case "US-STOCK":
		return core.HotCategoryUSSP500
	case "US-ETF":
		return core.HotCategoryUSETF
	case "HK-MAIN", "HK-GEM":
		return core.HotCategoryHK
	case "HK-ETF":
		return core.HotCategoryHKETF
	case "CN-ETF":
		return core.HotCategoryCNETF
	default:
		return core.HotCategoryCNA
	}
}
