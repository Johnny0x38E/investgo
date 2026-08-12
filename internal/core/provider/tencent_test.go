package provider

import (
	"encoding/json"
	"testing"

	"investgo/internal/core"
)

func TestTencentParseRawFloat(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want float64
	}{
		{name: "number", raw: "16.88", want: 16.88},
		{name: "quoted", raw: `"16.88"`, want: 16.88},
		{name: "blank", raw: `""`, want: 0},
		{name: "dash", raw: `"-"`, want: 0},
		{name: "null", raw: "null", want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tencentParseRawFloat(json.RawMessage(tc.raw))
			if err != nil || got != tc.want {
				t.Fatalf("tencentParseRawFloat(%s) = %v, %v; want %v, nil", tc.raw, got, err, tc.want)
			}
		})
	}

	if _, err := tencentParseRawFloat(json.RawMessage(`"bad"`)); err == nil {
		t.Fatal("malformed numeric value returned nil error")
	}
}

func TestTencentKlineRowRejectsMalformedNumericValue(t *testing.T) {
	var row tencentKlineRow
	err := json.Unmarshal([]byte(`["2026-01-01", "bad", "2", "3", "1", "100"]`), &row)
	if err == nil {
		t.Fatal("malformed K-line row returned nil error")
	}
}

func TestBuildTencentQuoteUsesMarketSpecificCurrencyField(t *testing.T) {
	cases := []struct {
		name        string
		market      string
		currencyIdx int
		currency    string
	}{
		{name: "hong kong", market: "HK-MAIN", currencyIdx: 75, currency: "HKD"},
		{name: "mainland china", market: "CN-A", currencyIdx: 82, currency: "CNY"},
		{name: "united states", market: "US-STOCK", currencyIdx: 35, currency: "USD"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := make([]string, 83)
			fields[1] = "Example"
			fields[3] = "10"
			fields[4] = "9"
			fields[5] = "9.5"
			fields[30] = "2026/08/05 13:37:26"
			fields[31] = "1"
			fields[32] = "11.11"
			fields[33] = "11"
			fields[34] = "8"
			fields[35] = "10"
			fields[36] = "100"
			fields[tc.currencyIdx] = tc.currency

			item := core.WatchlistItem{
				Symbol:   "00001.HK",
				Name:     "Example",
				Market:   tc.market,
				Currency: "fallback",
			}
			target := core.QuoteTarget{
				Key:           item.Symbol,
				DisplaySymbol: item.Symbol,
				Market:        tc.market,
				Currency:      tc.currency,
			}

			quote, ok := buildTencentQuote(item, target, fields)
			if !ok {
				t.Fatal("buildTencentQuote returned no quote")
			}
			if quote.Currency != tc.currency {
				t.Fatalf("currency = %q; want %q", quote.Currency, tc.currency)
			}
		})
	}
}
