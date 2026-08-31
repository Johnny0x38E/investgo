package instrument

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AssetClass identifies the broad kind of tradable instrument.
type AssetClass string

const (
	AssetClassEquity AssetClass = "equity"
	AssetClassETF    AssetClass = "etf"
	AssetClassCrypto AssetClass = "crypto"
)

// InstrumentStatus describes whether an instrument remains available for use.
type InstrumentStatus string

const (
	InstrumentStatusActive   InstrumentStatus = "active"
	InstrumentStatusInactive InstrumentStatus = "inactive"
	InstrumentStatusDelisted InstrumentStatus = "delisted"
)

var ErrInvalidInstrument = errors.New("invalid instrument")

// ValidationError identifies the field that violates an instrument invariant.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid instrument %s: %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return ErrInvalidInstrument
}

// Identity is the provider-independent key of an instrument.
type Identity struct {
	AssetClass AssetClass
	Market     string
	Exchange   string
	Symbol     string
}

// Instrument is the canonical tradable identity shared by pools, watchlists,
// holdings, alerts, and recommendations.
type Instrument struct {
	ID            string
	AssetClass    AssetClass
	Symbol        string
	Name          string
	Market        string
	Exchange      string
	BaseAsset     string
	QuoteCurrency string
	Status        InstrumentStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (i Instrument) Identity() Identity {
	return Identity{
		AssetClass: i.AssetClass,
		Market:     i.Market,
		Exchange:   i.Exchange,
		Symbol:     i.Symbol,
	}
}

// ProviderSymbol maps one canonical instrument to the symbol syntax expected by
// a market-data provider.
type ProviderSymbol struct {
	InstrumentID string
	ProviderID   string
	Symbol       string
	MetadataJSON string
	UpdatedAt    time.Time
}

// Normalize converts supported aliases and provider-shaped symbols into one
// canonical instrument representation.
func Normalize(value Instrument) (Instrument, error) {
	value.ID = strings.TrimSpace(value.ID)
	value.AssetClass = AssetClass(strings.ToLower(strings.TrimSpace(string(value.AssetClass))))
	if !isValidAssetClass(value.AssetClass) {
		return Instrument{}, invalid("assetClass", "must be equity, etf, or crypto")
	}

	market, err := normalizeMarket(value.Market, value.AssetClass)
	if err != nil {
		return Instrument{}, err
	}
	value.Market = market

	value.Symbol, value.Exchange, value.Market, err = normalizeListingIdentity(
		value.Symbol,
		value.Exchange,
		value.Market,
		value.AssetClass,
	)
	if err != nil {
		return Instrument{}, err
	}

	value.Name = strings.TrimSpace(value.Name)
	if value.Name == "" {
		value.Name = value.Symbol
	}
	value.BaseAsset = strings.ToUpper(strings.TrimSpace(value.BaseAsset))
	value.QuoteCurrency = strings.ToUpper(strings.TrimSpace(value.QuoteCurrency))
	if value.AssetClass == AssetClassCrypto {
		parts := strings.Split(value.Symbol, "-")
		if value.BaseAsset == "" && len(parts) == 2 {
			value.BaseAsset = parts[0]
		}
		if value.QuoteCurrency == "" && len(parts) == 2 {
			value.QuoteCurrency = parts[1]
		}
		if value.BaseAsset == "" {
			return Instrument{}, invalid("baseAsset", "is required for crypto instruments")
		}
		if value.QuoteCurrency == "" {
			return Instrument{}, invalid("quoteCurrency", "is required for crypto instruments")
		}
	} else {
		value.BaseAsset = ""
		if value.QuoteCurrency == "" {
			value.QuoteCurrency = defaultCurrency(value.Market)
		}
	}

	value.Status = InstrumentStatus(strings.ToLower(strings.TrimSpace(string(value.Status))))
	if value.Status == "" {
		value.Status = InstrumentStatusActive
	}
	if !isValidStatus(value.Status) {
		return Instrument{}, invalid("status", "must be active, inactive, or delisted")
	}
	if !value.CreatedAt.IsZero() {
		value.CreatedAt = value.CreatedAt.UTC()
	}
	if !value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.UpdatedAt.UTC()
	}
	return value, nil
}

// NormalizeIdentity applies the same canonicalization used by Instrument
// persistence without requiring metadata such as name or timestamps.
func NormalizeIdentity(value Identity) (Identity, error) {
	normalized, err := Normalize(Instrument{
		AssetClass: value.AssetClass,
		Market:     value.Market,
		Exchange:   value.Exchange,
		Symbol:     value.Symbol,
	})
	if err != nil {
		return Identity{}, err
	}
	return normalized.Identity(), nil
}

// CanonicalID returns a deterministic ID for a canonical identity.
func CanonicalID(value Identity) (string, error) {
	normalized, err := NormalizeIdentity(value)
	if err != nil {
		return "", err
	}
	key := strings.Join([]string{
		string(normalized.AssetClass),
		normalized.Market,
		normalized.Exchange,
		normalized.Symbol,
	}, "|")
	digest := sha256.Sum256([]byte(key))
	return "instrument-" + hex.EncodeToString(digest[:16]), nil
}

// NormalizeProviderSymbol validates and canonicalizes provider mapping metadata.
func NormalizeProviderSymbol(value ProviderSymbol) (ProviderSymbol, error) {
	value.InstrumentID = strings.TrimSpace(value.InstrumentID)
	if value.InstrumentID == "" {
		return ProviderSymbol{}, invalid("instrumentId", "is required")
	}
	value.ProviderID = strings.ToLower(strings.TrimSpace(value.ProviderID))
	if value.ProviderID == "" {
		return ProviderSymbol{}, invalid("providerId", "is required")
	}
	value.Symbol = strings.TrimSpace(value.Symbol)
	if value.Symbol == "" {
		return ProviderSymbol{}, invalid("providerSymbol", "is required")
	}
	value.MetadataJSON = strings.TrimSpace(value.MetadataJSON)
	if value.MetadataJSON == "" {
		value.MetadataJSON = "{}"
	}
	if !json.Valid([]byte(value.MetadataJSON)) {
		return ProviderSymbol{}, invalid("metadataJson", "must contain valid JSON")
	}
	if !value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.UpdatedAt.UTC()
	}
	return value, nil
}

func normalizeMarket(raw string, assetClass AssetClass) (string, error) {
	market := strings.ToUpper(strings.TrimSpace(raw))
	market = strings.ReplaceAll(market, "_", "-")
	switch market {
	case "A-SHARE", "ASHARE", "CN", "A", "CN-A":
		market = "CN-A"
	case "CN-GEM", "GEM":
		market = "CN-GEM"
	case "CN-STAR", "STAR":
		market = "CN-STAR"
	case "CN-ETF", "CNETF":
		market = "CN-ETF"
	case "CN-BJ", "BJ":
		market = "CN-BJ"
	case "HK", "H-SHARE", "HK-MAIN":
		market = "HK-MAIN"
	case "HK-GEM":
		market = "HK-GEM"
	case "HK-ETF":
		market = "HK-ETF"
	case "US", "NASDAQ", "NYSE", "US-NYQ", "US-STOCK":
		market = "US-STOCK"
	case "US ETF", "ETF", "US-ETF":
		market = "US-ETF"
	case "CRYPTO", "SPOT", "CRYPTO-SPOT":
		market = "CRYPTO-SPOT"
	}
	if market == "" {
		return "", invalid("market", "is required")
	}

	switch assetClass {
	case AssetClassEquity:
		if !isEquityMarket(market) {
			return "", invalid("market", fmt.Sprintf("%s is not an equity market", market))
		}
	case AssetClassETF:
		if !isETFMarket(market) {
			return "", invalid("market", fmt.Sprintf("%s is not an ETF market", market))
		}
	case AssetClassCrypto:
		if market != "CRYPTO-SPOT" {
			return "", invalid("market", fmt.Sprintf("%s is not a crypto market", market))
		}
	}
	return market, nil
}

func normalizeListingIdentity(symbol, exchange, market string, assetClass AssetClass) (string, string, string, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	symbol = strings.ReplaceAll(symbol, " ", "")
	if symbol == "" {
		return "", "", "", invalid("symbol", "is required")
	}

	switch {
	case strings.HasPrefix(market, "CN-"):
		return normalizeCNIdentity(symbol, exchange, market, assetClass)
	case strings.HasPrefix(market, "HK-"):
		return normalizeHKIdentity(symbol, exchange, market)
	case strings.HasPrefix(market, "US-"):
		return normalizeUSIdentity(symbol, exchange, market)
	case market == "CRYPTO-SPOT":
		return normalizeCryptoIdentity(symbol, exchange, market)
	default:
		return "", "", "", invalid("market", fmt.Sprintf("unsupported market %s", market))
	}
}

func normalizeCNIdentity(symbol, exchange, market string, assetClass AssetClass) (string, string, string, error) {
	var symbolExchange string
	for _, candidate := range []struct {
		prefix   string
		suffix   string
		exchange string
	}{
		{prefix: "SH", suffix: ".SH", exchange: "SSE"},
		{prefix: "SZ", suffix: ".SZ", exchange: "SZSE"},
		{prefix: "BJ", suffix: ".BJ", exchange: "BSE"},
	} {
		if strings.HasPrefix(symbol, candidate.prefix) {
			symbol = strings.TrimPrefix(symbol, candidate.prefix)
			symbolExchange = candidate.exchange
			break
		}
		if strings.HasSuffix(symbol, candidate.suffix) {
			symbol = strings.TrimSuffix(symbol, candidate.suffix)
			symbolExchange = candidate.exchange
			break
		}
	}
	if len(symbol) != 6 || !isDigits(symbol) {
		return "", "", "", invalid("symbol", "A-share and onshore ETF symbols must contain exactly 6 digits")
	}

	inferredMarket, inferredExchange, err := inferCNIdentity(symbol)
	if err != nil {
		return "", "", "", err
	}
	if assetClass == AssetClassETF {
		if inferredMarket != "CN-ETF" {
			return "", "", "", invalid("symbol", fmt.Sprintf("%s is not recognized as an onshore ETF", symbol))
		}
		market = "CN-ETF"
	} else {
		if inferredMarket == "CN-ETF" {
			return "", "", "", invalid("market", fmt.Sprintf("%s is an ETF symbol", symbol))
		}
		market = inferredMarket
	}

	normalizedExchange := normalizeExchange(exchange)
	if normalizedExchange == "" {
		normalizedExchange = symbolExchange
	}
	if normalizedExchange == "" {
		normalizedExchange = inferredExchange
	}
	if normalizedExchange != inferredExchange || (symbolExchange != "" && symbolExchange != inferredExchange) {
		return "", "", "", invalid("exchange", fmt.Sprintf("does not match symbol %s", symbol))
	}
	return symbol, normalizedExchange, market, nil
}

func normalizeHKIdentity(symbol, exchange, market string) (string, string, string, error) {
	symbol = strings.TrimPrefix(symbol, "HK")
	symbol = strings.TrimSuffix(symbol, ".HK")
	if len(symbol) == 0 || len(symbol) > 5 || !isDigits(symbol) {
		return "", "", "", invalid("symbol", "Hong Kong symbols must contain 1 to 5 digits")
	}
	symbol = strings.Repeat("0", 5-len(symbol)) + symbol
	normalizedExchange := normalizeExchange(exchange)
	if normalizedExchange == "" {
		normalizedExchange = "HKEX"
	}
	if normalizedExchange != "HKEX" {
		return "", "", "", invalid("exchange", "does not match a Hong Kong listing")
	}
	return symbol, normalizedExchange, market, nil
}

func normalizeUSIdentity(symbol, exchange, market string) (string, string, string, error) {
	symbol = strings.ReplaceAll(symbol, ".", "-")
	for _, r := range symbol {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return "", "", "", invalid("symbol", "US symbols may contain only letters, digits, and hyphens")
		}
	}
	if strings.HasPrefix(symbol, "-") || strings.HasSuffix(symbol, "-") || strings.Contains(symbol, "--") {
		return "", "", "", invalid("symbol", "US symbol separators are invalid")
	}
	return symbol, normalizeExchange(exchange), market, nil
}

func normalizeCryptoIdentity(symbol, exchange, market string) (string, string, string, error) {
	symbol = strings.NewReplacer("/", "-", "_", "-").Replace(symbol)
	parts := strings.Split(symbol, "-")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", invalid("symbol", "crypto symbols must be BASE-QUOTE pairs")
	}
	exchange = normalizeExchange(exchange)
	if exchange == "" {
		return "", "", "", invalid("exchange", "is required for crypto instruments")
	}
	return symbol, exchange, market, nil
}

func inferCNIdentity(symbol string) (string, string, error) {
	switch {
	case strings.HasPrefix(symbol, "688"), strings.HasPrefix(symbol, "689"):
		return "CN-STAR", "SSE", nil
	case symbol[0] == '6', symbol[0] == '9':
		return "CN-A", "SSE", nil
	case symbol[0] == '5':
		return "CN-ETF", "SSE", nil
	case symbol[0] == '3':
		return "CN-GEM", "SZSE", nil
	case strings.HasPrefix(symbol, "15"), strings.HasPrefix(symbol, "16"):
		return "CN-ETF", "SZSE", nil
	case symbol[0] == '0', symbol[0] == '1', symbol[0] == '2':
		return "CN-A", "SZSE", nil
	case symbol[0] == '4', symbol[0] == '8':
		return "CN-BJ", "BSE", nil
	default:
		return "", "", invalid("symbol", fmt.Sprintf("cannot infer a supported Chinese listing from %s", symbol))
	}
}

func normalizeExchange(raw string) string {
	exchange := strings.ToUpper(strings.TrimSpace(raw))
	switch exchange {
	case "SH", "SSE", "XSHG":
		return "SSE"
	case "SZ", "SZSE", "XSHE":
		return "SZSE"
	case "BJ", "BSE", "XBEI":
		return "BSE"
	case "HK", "HKEX", "XHKG":
		return "HKEX"
	case "NYQ", "NYSE", "XNYS":
		return "NYSE"
	case "NMS", "NGM", "NASDAQ", "XNAS":
		return "NASDAQ"
	case "ARCX", "ARCA", "NYSEARCA":
		return "NYSEARCA"
	default:
		return exchange
	}
}

func defaultCurrency(market string) string {
	switch {
	case strings.HasPrefix(market, "CN-"):
		return "CNY"
	case strings.HasPrefix(market, "HK-"):
		return "HKD"
	case strings.HasPrefix(market, "US-"):
		return "USD"
	default:
		return ""
	}
}

func isEquityMarket(market string) bool {
	switch market {
	case "CN-A", "CN-GEM", "CN-STAR", "CN-BJ", "HK-MAIN", "HK-GEM", "US-STOCK":
		return true
	default:
		return false
	}
}

func isETFMarket(market string) bool {
	switch market {
	case "CN-ETF", "HK-ETF", "US-ETF":
		return true
	default:
		return false
	}
}

func isValidAssetClass(assetClass AssetClass) bool {
	switch assetClass {
	case AssetClassEquity, AssetClassETF, AssetClassCrypto:
		return true
	default:
		return false
	}
}

func isValidStatus(status InstrumentStatus) bool {
	switch status {
	case InstrumentStatusActive, InstrumentStatusInactive, InstrumentStatusDelisted:
		return true
	default:
		return false
	}
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func invalid(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
