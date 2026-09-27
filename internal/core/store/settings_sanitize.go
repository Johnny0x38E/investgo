package store

import (
	"errors"
	"net/url"
	"slices"
	"strings"

	"investgo/internal/core"
)

// SettingsUpdate is the JSON body for PUT /api/settings.
//
// The route is a partial update. A nil field was omitted from the body and
// keeps the stored value. The Vue settings form sends every field on save, so
// an empty proxyURL or API key is an intentional clear. Other empty strings
// are ignored because those settings are required enums. A present
// hotCacheTTLSeconds replaces the stored TTL. Values below
// core.MinHotCacheTTLSeconds are rejected. Values above
// core.MaxHotCacheTTLSeconds (one hour) are clamped.
// developerMode and useNativeTitleBar change only when the JSON includes them,
// so a partial body cannot reset them to false.
type SettingsUpdate struct {
	HotCacheTTLSeconds *int    `json:"hotCacheTTLSeconds"`
	CNQuoteSource      *string `json:"cnQuoteSource"`
	HKQuoteSource      *string `json:"hkQuoteSource"`
	USQuoteSource      *string `json:"usQuoteSource"`
	ThemeMode          *string `json:"themeMode"`
	ColorTheme         *string `json:"colorTheme"`
	FontPreset         *string `json:"fontPreset"`
	AmountDisplay      *string `json:"amountDisplay"`
	CurrencyDisplay    *string `json:"currencyDisplay"`
	PriceColorScheme   *string `json:"priceColorScheme"`
	Locale             *string `json:"locale"`
	ProxyMode          *string `json:"proxyMode"`
	ProxyURL           *string `json:"proxyURL"`
	AlphaVantageAPIKey *string `json:"alphaVantageApiKey"`
	TwelveDataAPIKey   *string `json:"twelveDataApiKey"`
	FinnhubAPIKey      *string `json:"finnhubApiKey"`
	TiingoAPIKey       *string `json:"tiingoApiKey"`
	PolygonAPIKey      *string `json:"polygonApiKey"`
	DeveloperMode      *bool   `json:"developerMode"`
	DashboardCurrency  *string `json:"dashboardCurrency"`
	UseNativeTitleBar  *bool   `json:"useNativeTitleBar"`
}

// sanitiseSettings merges a partial settings update with current configuration and validates the result.
func sanitiseSettings(
	input SettingsUpdate,
	current core.AppSettings,
	quoteProviders map[string]core.QuoteProvider,
	quoteSourceOptions []core.QuoteSourceOption,
) (core.AppSettings, error) {
	settings := current
	if input.HotCacheTTLSeconds != nil {
		settings.HotCacheTTLSeconds = *input.HotCacheTTLSeconds
	}
	settings.CNQuoteSource = applyOptionalString(settings.CNQuoteSource, input.CNQuoteSource, strings.ToLower)
	settings.HKQuoteSource = applyOptionalString(settings.HKQuoteSource, input.HKQuoteSource, strings.ToLower)
	settings.USQuoteSource = applyOptionalString(settings.USQuoteSource, input.USQuoteSource, strings.ToLower)
	settings.ThemeMode = applyOptionalString(settings.ThemeMode, input.ThemeMode, strings.ToLower)
	settings.ColorTheme = applyOptionalString(settings.ColorTheme, input.ColorTheme, strings.ToLower)
	settings.FontPreset = applyOptionalString(settings.FontPreset, input.FontPreset, strings.ToLower)
	settings.AmountDisplay = applyOptionalString(settings.AmountDisplay, input.AmountDisplay, strings.ToLower)
	settings.CurrencyDisplay = applyOptionalString(settings.CurrencyDisplay, input.CurrencyDisplay, strings.ToLower)
	settings.PriceColorScheme = applyOptionalString(settings.PriceColorScheme, input.PriceColorScheme, strings.ToLower)
	settings.Locale = applyOptionalString(settings.Locale, input.Locale, nil)
	settings.ProxyMode = applyOptionalString(settings.ProxyMode, input.ProxyMode, strings.ToLower)
	settings.ProxyURL = applyClearableString(settings.ProxyURL, input.ProxyURL)
	settings.AlphaVantageAPIKey = applyClearableString(settings.AlphaVantageAPIKey, input.AlphaVantageAPIKey)
	settings.TwelveDataAPIKey = applyClearableString(settings.TwelveDataAPIKey, input.TwelveDataAPIKey)
	settings.FinnhubAPIKey = applyClearableString(settings.FinnhubAPIKey, input.FinnhubAPIKey)
	settings.TiingoAPIKey = applyClearableString(settings.TiingoAPIKey, input.TiingoAPIKey)
	settings.PolygonAPIKey = applyClearableString(settings.PolygonAPIKey, input.PolygonAPIKey)
	settings.DashboardCurrency = applyOptionalString(settings.DashboardCurrency, input.DashboardCurrency, strings.ToUpper)
	if input.DeveloperMode != nil {
		settings.DeveloperMode = *input.DeveloperMode
	}
	if input.UseNativeTitleBar != nil {
		settings.UseNativeTitleBar = *input.UseNativeTitleBar
	}

	if settings.HotCacheTTLSeconds < core.MinHotCacheTTLSeconds {
		return core.AppSettings{}, errors.New("Cache TTL must be at least 10 seconds")
	}
	settings.HotCacheTTLSeconds = core.ClampHotCacheTTLSeconds(settings.HotCacheTTLSeconds)
	settings.CNQuoteSource = normaliseQuoteSourceIDForSettings(settings.CNQuoteSource, "CN-A", quoteProviders, quoteSourceOptions)
	settings.HKQuoteSource = normaliseQuoteSourceIDForSettings(settings.HKQuoteSource, "HK-MAIN", quoteProviders, quoteSourceOptions)
	settings.USQuoteSource = normaliseQuoteSourceIDForSettings(settings.USQuoteSource, "US-STOCK", quoteProviders, quoteSourceOptions)
	if len(quoteProviders) > 0 {
		if _, ok := quoteProviders[settings.CNQuoteSource]; !ok {
			return core.AppSettings{}, errors.New("China quote source is invalid")
		}
		if _, ok := quoteProviders[settings.HKQuoteSource]; !ok {
			return core.AppSettings{}, errors.New("Hong Kong quote source is invalid")
		}
		if _, ok := quoteProviders[settings.USQuoteSource]; !ok {
			return core.AppSettings{}, errors.New("US quote source is invalid")
		}
	}
	switch settings.USQuoteSource {
	case "alpha-vantage":
		if settings.AlphaVantageAPIKey == "" {
			return core.AppSettings{}, errors.New("Alpha Vantage API key is required")
		}
	case "twelve-data":
		if settings.TwelveDataAPIKey == "" {
			return core.AppSettings{}, errors.New("Twelve Data API key is required")
		}
	case "finnhub":
		if settings.FinnhubAPIKey == "" {
			return core.AppSettings{}, errors.New("Finnhub API key is required")
		}
	case "tiingo":
		if settings.TiingoAPIKey == "" {
			return core.AppSettings{}, errors.New("Tiingo API key is required")
		}
	case "polygon":
		if settings.PolygonAPIKey == "" {
			return core.AppSettings{}, errors.New("Polygon API key is required")
		}
	}

	switch settings.FontPreset {
	case "", "system":
		settings.FontPreset = "system"
	case "reading", "compact":
	default:
		return core.AppSettings{}, errors.New("Font preset must be one of: system / reading / compact")
	}

	switch settings.ThemeMode {
	case "", "system":
		settings.ThemeMode = "system"
	case "light", "dark":
	default:
		return core.AppSettings{}, errors.New("Theme mode must be one of: system / light / dark")
	}

	switch settings.ColorTheme {
	case "", "blue":
		settings.ColorTheme = "blue"
	case "graphite", "forest", "sunset", "rose", "violet", "amber", "ocean", "mint", "coral", "indigo":
	default:
		return core.AppSettings{}, errors.New("Color theme must be one of: blue / graphite / forest / sunset / rose / violet / amber / ocean / mint / coral / indigo")
	}

	switch settings.AmountDisplay {
	case "", "full":
		settings.AmountDisplay = "full"
	case "compact":
	default:
		return core.AppSettings{}, errors.New("Amount display must be one of: full / compact")
	}

	switch settings.CurrencyDisplay {
	case "", "symbol":
		settings.CurrencyDisplay = "symbol"
	case "code":
	default:
		return core.AppSettings{}, errors.New("Currency display must be one of: symbol / code")
	}

	switch settings.PriceColorScheme {
	case "", "cn":
		settings.PriceColorScheme = "cn"
	case "intl":
	default:
		return core.AppSettings{}, errors.New("Price color scheme must be one of: cn / intl")
	}

	switch settings.Locale {
	case "", "system":
		settings.Locale = "system"
	case "zh-CN", "en-US":
	default:
		return core.AppSettings{}, errors.New("Locale must be one of: system / zh-CN / en-US")
	}

	switch settings.ProxyMode {
	case "":
		settings.ProxyMode = "system"
		settings.ProxyURL = ""
	case "none":
		settings.ProxyMode = "none"
		settings.ProxyURL = ""
	case "system":
		settings.ProxyURL = ""
	case "custom":
		if settings.ProxyURL == "" {
			return core.AppSettings{}, errors.New("Custom proxy URL is required")
		}
		parsed, err := url.Parse(settings.ProxyURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return core.AppSettings{}, errors.New("Custom proxy URL is invalid")
		}
	default:
		return core.AppSettings{}, errors.New("Proxy mode must be one of: none / system / custom")
	}

	switch settings.DashboardCurrency {
	case "", "CNY":
		settings.DashboardCurrency = "CNY"
	case "HKD", "USD":
	default:
		return core.AppSettings{}, errors.New("Dashboard currency must be one of: CNY / HKD / USD")
	}

	return settings, nil
}

// applyOptionalString keeps current when the field was omitted or blank.
// Blank required enums are not a clear signal; only proxy URL and API keys clear on "".
func applyOptionalString(current string, next *string, transform func(string) string) string {
	if next == nil {
		return current
	}
	value := strings.TrimSpace(*next)
	if value == "" {
		return current
	}
	if transform != nil {
		return transform(value)
	}
	return value
}

// applyClearableString keeps current when the field was omitted.
// A present value, including "", replaces the stored secret or proxy URL.
func applyClearableString(current string, next *string) string {
	if next == nil {
		return current
	}
	return strings.TrimSpace(*next)
}

// normaliseQuoteSourceIDForSettings determines the final quote source ID to use based on user input, market type, and available quote source list.
func normaliseQuoteSourceIDForSettings(
	sourceID string,
	market string,
	providers map[string]core.QuoteProvider,
	options []core.QuoteSourceOption,
) string {
	sourceID = strings.ToLower(strings.TrimSpace(sourceID))
	if sourceID != "" {
		if _, ok := providers[sourceID]; ok && quoteSourceSupportsMarketForSettings(sourceID, market, options) {
			return sourceID
		}
	}
	switch marketGroupForMarket(market) {
	case "hk":
		if _, ok := providers[core.DefaultHKQuoteSourceID]; ok {
			return core.DefaultHKQuoteSourceID
		}
	case "us":
		if _, ok := providers[core.DefaultUSQuoteSourceID]; ok {
			return core.DefaultUSQuoteSourceID
		}
	default:
		if _, ok := providers[core.DefaultCNQuoteSourceID]; ok {
			return core.DefaultCNQuoteSourceID
		}
	}
	if _, ok := providers[core.DefaultQuoteSourceID]; ok {
		return core.DefaultQuoteSourceID
	}
	for id := range providers {
		return id
	}
	return core.DefaultQuoteSourceID
}

// quoteSourceSupportsMarketForSettings returns whether the given quote source supports the specified market
// by consulting the authoritative QuoteSourceOption.SupportedMarkets list.
func quoteSourceSupportsMarketForSettings(sourceID string, market string, options []core.QuoteSourceOption) bool {
	for _, opt := range options {
		if opt.ID == sourceID {
			if len(opt.SupportedMarkets) == 0 {
				return true
			}
			return slices.Contains(opt.SupportedMarkets, market)
		}
	}
	return false
}
