// tiingo.go — Tiingo quote and history provider (US only, API token required).
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"investgo/internal/common/errs"
	"investgo/internal/core"
	"investgo/internal/core/endpoint"
)

const tiingoQuoteBatchSize = 100

type TiingoQuoteProvider struct {
	client   *http.Client
	settings func() core.AppSettings
}

type TiingoHistoryProvider struct {
	client   *http.Client
	settings func() core.AppSettings
}

type tiingoIEXQuote struct {
	Ticker            string   `json:"ticker"`
	Timestamp         string   `json:"timestamp"`
	QuoteTimestamp    string   `json:"quoteTimestamp"`
	LastSaleTimestamp string   `json:"lastSaleTimestamp"`
	Last              *float64 `json:"last"`
	TngoLast          *float64 `json:"tngoLast"`
	PrevClose         *float64 `json:"prevClose"`
	Open              *float64 `json:"open"`
	High              *float64 `json:"high"`
	Low               *float64 `json:"low"`
	Volume            *float64 `json:"volume"`
}

type tiingoEODBar struct {
	Date     string   `json:"date"`
	Open     float64  `json:"open"`
	High     float64  `json:"high"`
	Low      float64  `json:"low"`
	Close    float64  `json:"close"`
	Volume   float64  `json:"volume"`
	AdjClose *float64 `json:"adjClose"`
}

type tiingoIntradayBar struct {
	Date   string  `json:"date"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

func NewTiingoQuoteProvider(client *http.Client, settings func() core.AppSettings) *TiingoQuoteProvider {
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	if settings == nil {
		settings = func() core.AppSettings { return core.AppSettings{} }
	}
	return &TiingoQuoteProvider{client: client, settings: settings}
}

func (p *TiingoQuoteProvider) Name() string { return "Tiingo" }

func (p *TiingoQuoteProvider) Fetch(ctx context.Context, items []core.WatchlistItem) (map[string]core.Quote, error) {
	apiKey := strings.TrimSpace(p.settings().TiingoAPIKey)
	if apiKey == "" {
		return nil, errors.New("Tiingo API key is required")
	}

	targets, problems := CollectQuoteTargets(items)
	quotes := make(map[string]core.Quote, len(targets))
	if len(targets) == 0 {
		return quotes, errs.JoinProblems(problems)
	}

	itemByKey := make(map[string]core.WatchlistItem, len(items))
	tiingoSymbols := make([]string, 0, len(targets))
	targetByTiingo := make(map[string]core.QuoteTarget, len(targets))
	for _, item := range items {
		target, err := core.ResolveQuoteTarget(item)
		if err != nil {
			continue
		}
		if target.Market != "US-STOCK" && target.Market != "US-ETF" {
			problems = append(problems, fmt.Sprintf("Tiingo does not support item: %s", target.DisplaySymbol))
			continue
		}
		symbol := toTiingoSymbol(target.DisplaySymbol)
		itemByKey[target.Key] = item
		tiingoSymbols = append(tiingoSymbols, symbol)
		targetByTiingo[strings.ToUpper(symbol)] = target
	}
	if len(tiingoSymbols) == 0 {
		return quotes, errs.JoinProblems(problems)
	}

	for _, batch := range ChunkStrings(tiingoSymbols, tiingoQuoteBatchSize) {
		batchQuotes, err := fetchTiingoIEXQuotes(ctx, p.client, batch, apiKey)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		for _, entry := range batchQuotes {
			target, ok := targetByTiingo[strings.ToUpper(strings.TrimSpace(entry.Ticker))]
			if !ok {
				continue
			}
			item := itemByKey[target.Key]
			quote, ok := buildTiingoQuote(entry, item, target)
			if !ok {
				continue
			}
			quotes[target.Key] = quote
		}
	}

	if len(quotes) == 0 && len(problems) == 0 {
		problems = append(problems, "Tiingo quote response is empty")
	}
	return quotes, errs.JoinProblems(problems)
}

func NewTiingoHistoryProvider(client *http.Client, settings func() core.AppSettings) *TiingoHistoryProvider {
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	if settings == nil {
		settings = func() core.AppSettings { return core.AppSettings{} }
	}
	return &TiingoHistoryProvider{client: client, settings: settings}
}

func (p *TiingoHistoryProvider) Name() string { return "Tiingo" }

func (p *TiingoHistoryProvider) Fetch(ctx context.Context, item core.WatchlistItem, interval core.HistoryInterval) (core.HistorySeries, error) {
	apiKey := strings.TrimSpace(p.settings().TiingoAPIKey)
	if apiKey == "" {
		return core.HistorySeries{}, errors.New("Tiingo API key is required")
	}
	target, err := core.ResolveQuoteTarget(item)
	if err != nil {
		return core.HistorySeries{}, err
	}
	if target.Market != "US-STOCK" && target.Market != "US-ETF" {
		return core.HistorySeries{}, fmt.Errorf("Tiingo does not support market: %s", target.DisplaySymbol)
	}

	symbol := toTiingoSymbol(target.DisplaySymbol)
	var points []core.HistoryPoint
	switch interval {
	case core.HistoryRange1h, core.HistoryRange1d:
		points, err = fetchTiingoIntradayHistory(ctx, p.client, symbol, interval, apiKey)
	default:
		points, err = fetchTiingoDailyHistory(ctx, p.client, symbol, interval, apiKey)
	}
	if err != nil {
		return core.HistorySeries{}, err
	}
	if len(points) == 0 {
		return core.HistorySeries{}, errors.New("History response contains no valid price points")
	}

	series := core.HistorySeries{
		Symbol:      item.Symbol,
		Name:        FirstNonEmpty(item.Name, item.Symbol),
		Market:      item.Market,
		Currency:    FirstNonEmpty(item.Currency, target.Currency),
		Interval:    interval,
		Source:      p.Name(),
		Points:      points,
		GeneratedAt: time.Now(),
	}
	ApplyHistorySummary(&series)
	return series, nil
}

func fetchTiingoIEXQuotes(
	ctx context.Context,
	client *http.Client,
	symbols []string,
	apiKey string,
) ([]tiingoIEXQuote, error) {
	params := url.Values{}
	params.Set("tickers", strings.Join(symbols, ","))
	params.Set("token", apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.URLWithQuery(endpoint.TiingoIEXAPI, params), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Tiingo quote request failed: %w", err)
	}
	defer resp.Body.Close() // nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Tiingo quote request failed: status %d", resp.StatusCode)
	}

	var parsed []tiingoIEXQuote
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, tiingoDecodeError("quote", body, err)
	}
	return parsed, nil
}

func buildTiingoQuote(entry tiingoIEXQuote, item core.WatchlistItem, target core.QuoteTarget) (core.Quote, bool) {
	current := firstPositiveFloat(derefFloat64(entry.TngoLast), derefFloat64(entry.Last))
	previous := derefFloat64(entry.PrevClose)
	open := derefFloat64(entry.Open)
	high := derefFloat64(entry.High)
	low := derefFloat64(entry.Low)
	if current <= 0 {
		return core.Quote{}, false
	}

	updatedAt := parseTiingoTimestamp(entry.LastSaleTimestamp)
	if updatedAt.IsZero() {
		updatedAt = parseTiingoTimestamp(entry.QuoteTimestamp)
	}
	if updatedAt.IsZero() {
		updatedAt = parseTiingoTimestamp(entry.Timestamp)
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	quote := BuildQuote(
		FirstNonEmpty(item.Name, target.DisplaySymbol),
		current,
		previous,
		open,
		high,
		low,
		updatedAt,
		"Tiingo",
	)
	quote.Symbol = target.DisplaySymbol
	quote.Market = target.Market
	quote.Currency = FirstNonEmpty(item.Currency, target.Currency, "USD")
	quote.Volume = derefFloat64(entry.Volume)
	return quote, true
}

func fetchTiingoDailyHistory(
	ctx context.Context,
	client *http.Client,
	symbol string,
	interval core.HistoryInterval,
	apiKey string,
) ([]core.HistoryPoint, error) {
	now := time.Now().UTC()
	start := now.Add(-HistoryTrimWindow(interval))
	if interval == core.HistoryRangeAll || HistoryTrimWindow(interval) == 0 {
		start = now.AddDate(-20, 0, 0)
	}

	params := url.Values{}
	params.Set("startDate", start.Format("2006-01-02"))
	params.Set("endDate", now.Format("2006-01-02"))
	params.Set("token", apiKey)

	apiURL := endpoint.URLWithQuery(endpoint.TiingoDailyAPIPrefix+url.PathEscape(symbol)+"/prices", params)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Tiingo history request failed: %w", err)
	}
	defer resp.Body.Close() // nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Tiingo history request failed: status %d", resp.StatusCode)
	}

	var parsed []tiingoEODBar
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, tiingoDecodeError("history", body, err)
	}

	points := make([]core.HistoryPoint, 0, len(parsed))
	for _, bar := range parsed {
		closePrice := bar.Close
		if bar.AdjClose != nil && *bar.AdjClose > 0 {
			closePrice = *bar.AdjClose
		}
		if closePrice <= 0 {
			continue
		}
		ts := parseTiingoTimestamp(bar.Date)
		if ts.IsZero() {
			continue
		}
		points = append(points, core.HistoryPoint{
			Timestamp: ts,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     closePrice,
			Volume:    bar.Volume,
		})
	}
	return TrimHistoryPoints(points, HistoryTrimWindow(interval)), nil
}

func fetchTiingoIntradayHistory(
	ctx context.Context,
	client *http.Client,
	symbol string,
	interval core.HistoryInterval,
	apiKey string,
) ([]core.HistoryPoint, error) {
	now := time.Now().UTC()
	start := now.Add(-HistoryTrimWindow(interval))
	if start.IsZero() || HistoryTrimWindow(interval) == 0 {
		start = now.Add(-24 * time.Hour)
	}

	params := url.Values{}
	params.Set("startDate", start.Format("2006-01-02"))
	params.Set("resampleFreq", tiingoResampleFreq(interval))
	params.Set("token", apiKey)

	apiURL := endpoint.URLWithQuery(endpoint.TiingoIEXAPI+"/"+url.PathEscape(symbol)+"/prices", params)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Tiingo history request failed: %w", err)
	}
	defer resp.Body.Close() // nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Tiingo history request failed: status %d", resp.StatusCode)
	}

	var parsed []tiingoIntradayBar
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, tiingoDecodeError("history", body, err)
	}

	points := make([]core.HistoryPoint, 0, len(parsed))
	for _, bar := range parsed {
		if bar.Close <= 0 {
			continue
		}
		ts := parseTiingoTimestamp(bar.Date)
		if ts.IsZero() {
			continue
		}
		points = append(points, core.HistoryPoint{
			Timestamp: ts,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    bar.Volume,
		})
	}
	return TrimHistoryPoints(points, HistoryTrimWindow(interval)), nil
}

func tiingoResampleFreq(interval core.HistoryInterval) string {
	switch interval {
	case core.HistoryRange1h:
		return "5min"
	default:
		return "15min"
	}
}

func toTiingoSymbol(symbol string) string {
	return strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(symbol)), ".", "-")
}

func parseTiingoTimestamp(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		time.DateOnly,
	} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func tiingoDecodeError(kind string, body []byte, err error) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("Tiingo %s decode failed: %w", kind, err)
	}
	if msg[0] == '{' || msg[0] == '[' {
		return fmt.Errorf("Tiingo %s decode failed: %w", kind, err)
	}
	if len(msg) > 240 {
		msg = msg[:240] + "..."
	}
	return fmt.Errorf("Tiingo %s request failed: %s", kind, msg)
}
