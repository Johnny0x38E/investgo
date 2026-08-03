// provider_xueqiu.go — Xueqiu quote provider.
package provider

import (
	"context"
	"encoding/json"
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

type XueqiuQuoteProvider struct {
	client *http.Client
}

type xueqiuQuoteResponse struct {
	Data             []xueqiuQuoteItem `json:"data"`
	ErrorCode        int               `json:"error_code"`
	ErrorDescription string            `json:"error_description"`
}

type xueqiuQuoteItem struct {
	Symbol    string   `json:"symbol"`
	Name      string   `json:"name"`
	Current   *float64 `json:"current"`
	Percent   *float64 `json:"percent"`
	Chg       *float64 `json:"chg"`
	High      *float64 `json:"high"`
	Low       *float64 `json:"low"`
	Open      *float64 `json:"open"`
	LastClose *float64 `json:"last_close"`
	Timestamp *int64   `json:"timestamp"`
}

const xueqiuBatchSize = 50

func NewXueqiuQuoteProvider(client *http.Client) *XueqiuQuoteProvider {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	return &XueqiuQuoteProvider{client: client}
}

func (p *XueqiuQuoteProvider) Name() string {
	return "Xueqiu"
}

func (p *XueqiuQuoteProvider) Fetch(ctx context.Context, items []core.WatchlistItem) (map[string]core.Quote, error) {
	targets, problems := CollectQuoteTargets(items)
	quotes := make(map[string]core.Quote, len(targets))
	if len(targets) == 0 {
		return quotes, errs.JoinProblems(problems)
	}

	itemByKey := make(map[string]core.WatchlistItem, len(targets))
	xueqiuSymbols := make([]string, 0, len(targets))
	targetBySymbol := make(map[string]core.QuoteTarget, len(targets))
	for _, item := range items {
		target, err := core.ResolveQuoteTarget(item)
		if err != nil {
			continue
		}
		symbol, err := resolveXueqiuQuoteSymbol(target)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		itemByKey[target.Key] = item
		xueqiuSymbols = append(xueqiuSymbols, symbol)
		targetBySymbol[symbol] = target
	}

	if len(xueqiuSymbols) == 0 {
		return quotes, errs.JoinProblems(problems)
	}

	for _, batch := range ChunkStrings(xueqiuSymbols, xueqiuBatchSize) {
		params := url.Values{}
		params.Set("symbol", strings.Join(batch, ","))
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.XueqiuQuoteAPI+"?"+params.Encode(), nil)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
		request.Header.Set("Referer", endpoint.XueqiuReferer)
		request.Header.Set("Origin", endpoint.XueqiuOrigin)

		response, err := p.client.Do(request)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}

		body, err := io.ReadAll(response.Body)
		response.Body.Close() // nolint:errcheck
		if response.StatusCode != http.StatusOK {
			problems = append(problems, fmt.Sprintf("Xueqiu quote request failed: status %d", response.StatusCode))
			continue
		}
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}

		var parsed xueqiuQuoteResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if parsed.ErrorCode != 0 {
			problems = append(problems, fmt.Sprintf("Xueqiu quote error %d: %s", parsed.ErrorCode, parsed.ErrorDescription))
			continue
		}

		for _, entry := range parsed.Data {
			target, ok := targetBySymbol[strings.TrimSpace(entry.Symbol)]
			if !ok {
				continue
			}
			item := itemByKey[target.Key]
			current := derefFloat64(entry.Current)
			previous := derefFloat64(entry.LastClose)
			open := derefFloat64(entry.Open)
			high := derefFloat64(entry.High)
			low := derefFloat64(entry.Low)
			updatedAt := time.Now()
			if entry.Timestamp != nil && *entry.Timestamp > 0 {
				updatedAt = time.UnixMilli(*entry.Timestamp)
			}
			quote := BuildQuote(
				FirstNonEmpty(entry.Name, item.Name, target.DisplaySymbol),
				current,
				previous,
				open,
				high,
				low,
				updatedAt,
				p.Name(),
			)
			quote.Symbol = target.DisplaySymbol
			quote.Market = target.Market
			quote.Currency = target.Currency
			if quote.CurrentPrice <= 0 {
				continue
			}
			quotes[target.Key] = quote
		}
	}

	if len(quotes) == 0 && len(problems) == 0 {
		problems = append(problems, "Xueqiu quote response is empty")
	}
	return quotes, errs.JoinProblems(problems)
}

func derefFloat64(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func resolveXueqiuQuoteSymbol(target core.QuoteTarget) (string, error) {
	if code, ok := strings.CutSuffix(target.Key, ".SH"); ok {
		return "SH" + code, nil
	}
	if code, ok := strings.CutSuffix(target.Key, ".SZ"); ok {
		return "SZ" + code, nil
	}
	if strings.HasSuffix(target.Key, ".BJ") {
		return "", fmt.Errorf("Xueqiu does not support item: %s", target.DisplaySymbol)
	}
	if code, ok := strings.CutSuffix(target.Key, ".HK"); ok {
		return "HK" + code, nil
	}
	if target.Market == "US-STOCK" || target.Market == "US-ETF" {
		return target.DisplaySymbol, nil
	}
	return "", fmt.Errorf("Xueqiu does not support item: %s", target.DisplaySymbol)
}
