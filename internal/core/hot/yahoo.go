package hot

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"investgo/internal/common/errs"
	"investgo/internal/core/endpoint"
	"investgo/internal/core/provider"
)

// yahooSearchResponse models the JSON envelope returned by the Yahoo Finance search API.
type yahooSearchResponse struct {
	Quotes []struct {
		Symbol    string `json:"symbol"`
		ShortName string `json:"shortname"`
		LongName  string `json:"longname"`
		QuoteType string `json:"quoteType"`
		TypeDisp  string `json:"typeDisp"`
		Exchange  string `json:"exchange"`
		ExchDisp  string `json:"exchDisp"`
	} `json:"quotes"`
}

// searchYahooUSStockSeeds calls Yahoo Finance search API and returns US stock seeds matching the keyword.
// QuoteType must be EQUITY (or empty). Exchange filtering uses Yahoo MIC tokens
// (NMS/NYQ/…) — see isLikelyUSExchange. Class-share queries are hyphenated
// (BRK.B → BRK-B) by normaliseYahooSearchKeyword before the request.
func (s *HotService) searchYahooUSStockSeeds(ctx context.Context, keyword string) ([]hotSeed, error) {
	parsed, err := fetchYahooSearch(ctx, s.client, keyword)
	if err != nil {
		return nil, err
	}

	seeds := make([]hotSeed, 0, len(parsed.Quotes))
	seen := make(map[string]struct{}, len(parsed.Quotes))
	for _, quote := range parsed.Quotes {
		quoteType := strings.ToUpper(strings.TrimSpace(quote.QuoteType))
		if quoteType != "EQUITY" && quoteType != "" {
			continue
		}
		if !isLikelyUSExchange(quote.Exchange, quote.ExchDisp) {
			continue
		}

		symbol := strings.ToUpper(strings.TrimSpace(quote.Symbol))
		if symbol == "" {
			continue
		}

		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		seeds = append(seeds, hotSeed{
			Symbol:   symbol,
			Name:     provider.FirstNonEmpty(quote.LongName, quote.ShortName, symbol),
			Market:   "US-STOCK",
			Currency: "USD",
		})
	}
	return seeds, nil
}

// searchYahooUSSeeds fetches a list of US ETF instruments matching the keyword and filters for those likely listed on US exchanges.
func (s *HotService) searchYahooUSSeeds(ctx context.Context, keyword string) ([]hotSeed, error) {
	parsed, err := fetchYahooSearch(ctx, s.client, keyword)
	if err != nil {
		return nil, err
	}

	seeds := make([]hotSeed, 0, len(parsed.Quotes))
	seen := make(map[string]struct{}, len(parsed.Quotes))
	for _, quote := range parsed.Quotes {
		if !isYahooETFQuote(quote.QuoteType, quote.TypeDisp) || !isLikelyUSExchange(quote.Exchange, quote.ExchDisp) {
			continue
		}

		symbol := strings.ToUpper(strings.TrimSpace(quote.Symbol))
		if symbol == "" {
			continue
		}

		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		seeds = append(seeds, hotSeed{
			Symbol:   symbol,
			Name:     provider.FirstNonEmpty(quote.LongName, quote.ShortName, symbol),
			Market:   "US-ETF",
			Currency: "USD",
		})
	}
	return seeds, nil
}

// fetchYahooSearch queries the Yahoo Finance search API across all configured hosts
// and returns the first successful response. Hosts are raced concurrently so a
// single slow/unreachable host no longer blocks the whole search; the remaining
// in-flight requests are cancelled as soon as one succeeds.
func fetchYahooSearch(ctx context.Context, client *http.Client, keyword string) (yahooSearchResponse, error) {
	if client == nil {
		client = &http.Client{}
	}

	hosts := endpoint.YahooSearchHosts
	if len(hosts) == 0 {
		return yahooSearchResponse{}, errors.New("no Yahoo search hosts configured")
	}

	params := url.Values{}
	params.Set("q", normaliseYahooSearchKeyword(keyword))
	params.Set("quotesCount", "20")
	params.Set("newsCount", "0")
	params.Set("enableFuzzyQuery", "false")

	type hostResult struct {
		parsed yahooSearchResponse
		err    error
	}
	results := make([]hostResult, len(hosts))
	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		go func(idx int, h string) {
			defer wg.Done()
			parsed, err := fetchYahooSearchFromHost(ctx, client, h, params)
			results[idx] = hostResult{parsed: parsed, err: err}
		}(i, host)
	}
	wg.Wait()

	var problems []string
	for _, r := range results {
		if r.err == nil {
			return r.parsed, nil
		}
		problems = append(problems, r.err.Error())
	}
	return yahooSearchResponse{}, errs.JoinProblems(problems)
}

// fetchYahooSearchFromHost fetches search results from the specified Yahoo Search API host
// and parses them into the yahooSearchResponse struct.
func fetchYahooSearchFromHost(
	ctx context.Context,
	client *http.Client,
	host string,
	params url.Values,
) (yahooSearchResponse, error) {
	// Copy params to avoid mutating the shared slice across concurrent calls.
	query := make(url.Values, len(params))
	for key, values := range params {
		query[key] = append([]string(nil), values...)
	}

	requestURL := url.URL{
		Scheme:   "https",
		Host:     host,
		Path:     endpoint.YahooSearchPath,
		RawQuery: query.Encode(),
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return yahooSearchResponse{}, err
	}
	request.Header.Set(
		"User-Agent",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	)
	request.Header.Set("Origin", endpoint.YahooFinanceOrigin)
	request.Header.Set("Referer", endpoint.YahooFinanceReferer)

	response, err := client.Do(request)
	if err != nil {
		return yahooSearchResponse{}, err
	}
	defer response.Body.Close() // nolint:errcheck

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return yahooSearchResponse{}, err
	}

	if response.StatusCode != http.StatusOK {
		return yahooSearchResponse{}, fmt.Errorf("status %d", response.StatusCode)
	}

	var parsed yahooSearchResponse
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return yahooSearchResponse{}, err
	}
	return parsed, nil
}

// isYahooETFQuote reports whether the Yahoo Finance quote type fields indicate an ETF.
func isYahooETFQuote(quoteType, typeDisp string) bool {
	quoteType = strings.ToUpper(strings.TrimSpace(quoteType))
	typeDisp = strings.ToUpper(strings.TrimSpace(typeDisp))
	return quoteType == "ETF" || strings.Contains(typeDisp, "ETF")
}

// yahooUSExchangeTokens are the Yahoo search MIC / display fragments that mean
// a US listing. exchDisp is usually "NASDAQ" / "NYSE" / "NYSEArca" / "BATS Trading",
// but Exchange itself is a short MIC and will not contain those strings:
//
//	NMS / NGM / NCM — Nasdaq Global Select / Global / Capital
//	NYQ             — NYSE
//	ASE / AMEX      — NYSE American
//	PCX / ARCX      — NYSE Arca (SPY, IWM)
//	BTS / BATS      — Cboe BZX (ARKK)
//
// Pink-sheet / OTC tokens (PNK, OTCPK) are intentionally omitted.
var yahooUSExchangeTokens = []string{
	"NASDAQ", "NYSE", "ARCA", "ARCX", "BATS", "PCX",
	"NMS", "NGM", "NCM", "NYQ", "ASE", "AMEX", "BTS",
}

// isLikelyUSExchange reports whether the given exchange fields likely represent a US-listed
// instrument based on well-known US exchange identifiers.
func isLikelyUSExchange(exchange, exchDisp string) bool {
	label := strings.ToUpper(strings.TrimSpace(exchange + " " + exchDisp))
	if label == "" {
		return true
	}
	for _, token := range yahooUSExchangeTokens {
		if strings.Contains(label, token) {
			return true
		}
	}
	return false
}

// normaliseYahooSearchKeyword rewrites a US ticker into the form Yahoo search
// indexes. Share classes are listed with a hyphen (BRK-B, BF-B). Users and the
// local S&P pool write a dot (BRK.B); searching the dot form returns options
// and unrelated ETFs, not the equity.
func normaliseYahooSearchKeyword(keyword string) string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return keyword
	}
	upper := strings.ToUpper(keyword)
	dot := strings.LastIndexByte(upper, '.')
	if dot <= 0 || dot != len(upper)-2 {
		return keyword
	}
	class := upper[dot+1]
	if class < 'A' || class > 'Z' {
		return keyword
	}
	return upper[:dot] + "-" + upper[dot+1:]
}
