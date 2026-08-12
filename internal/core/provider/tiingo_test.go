package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"investgo/internal/core"
)

func TestToTiingoSymbol(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "aapl", want: "AAPL"},
		{in: "BRK.B", want: "BRK-B"},
		{in: "BRK-B", want: "BRK-B"},
		{in: "  msft ", want: "MSFT"},
	}
	for _, tc := range cases {
		if got := toTiingoSymbol(tc.in); got != tc.want {
			t.Fatalf("toTiingoSymbol(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseTiingoTimestamp(t *testing.T) {
	t.Parallel()

	got := parseTiingoTimestamp("2024-06-01T15:30:00.000Z")
	if got.IsZero() || !got.Equal(time.Date(2024, 6, 1, 15, 30, 0, 0, time.UTC)) {
		t.Fatalf("parseTiingoTimestamp RFC3339 = %v", got)
	}
	got = parseTiingoTimestamp("2024-06-01")
	if got.IsZero() || got.Format(time.DateOnly) != "2024-06-01" {
		t.Fatalf("parseTiingoTimestamp date-only = %v", got)
	}
	if !parseTiingoTimestamp("").IsZero() {
		t.Fatal("empty timestamp should be zero")
	}
}

func TestBuildTiingoQuotePrefersTngoLast(t *testing.T) {
	t.Parallel()

	tngoLast := 190.5
	last := 189.0
	prev := 188.0
	open := 188.5
	high := 191.0
	low := 187.5
	volume := 1_234_567.0

	item := core.WatchlistItem{Symbol: "AAPL", Name: "Apple", Market: "US-STOCK", Currency: "USD"}
	target := core.QuoteTarget{Key: "AAPL", DisplaySymbol: "AAPL", Market: "US-STOCK", Currency: "USD"}
	quote, ok := buildTiingoQuote(tiingoIEXQuote{
		Ticker:            "AAPL",
		TngoLast:          &tngoLast,
		Last:              &last,
		PrevClose:         &prev,
		Open:              &open,
		High:              &high,
		Low:               &low,
		Volume:            &volume,
		LastSaleTimestamp: "2024-06-01T15:30:00Z",
	}, item, target)
	if !ok {
		t.Fatal("buildTiingoQuote returned false")
	}
	if quote.CurrentPrice != tngoLast {
		t.Fatalf("CurrentPrice = %v; want %v", quote.CurrentPrice, tngoLast)
	}
	if quote.PreviousClose != prev {
		t.Fatalf("PreviousClose = %v; want %v", quote.PreviousClose, prev)
	}
	if quote.Volume != volume {
		t.Fatalf("Volume = %v; want %v", quote.Volume, volume)
	}
	if quote.Source != "Tiingo" {
		t.Fatalf("Source = %q; want Tiingo", quote.Source)
	}
	if quote.Currency != "USD" {
		t.Fatalf("Currency = %q; want USD", quote.Currency)
	}
}

func TestTiingoQuoteProviderFetchBatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/iex" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("token"); got != "test-token" {
			t.Fatalf("token = %q; want test-token", got)
		}
		tickers := strings.Split(r.URL.Query().Get("tickers"), ",")
		if len(tickers) != 2 {
			t.Fatalf("tickers = %#v; want 2 symbols", tickers)
		}
		lastA := 100.0
		prevA := 98.0
		lastB := 50.5
		prevB := 51.0
		_ = json.NewEncoder(w).Encode([]tiingoIEXQuote{
			{Ticker: "AAPL", Last: &lastA, PrevClose: &prevA, Timestamp: "2024-06-01T15:30:00Z"},
			{Ticker: "MSFT", Last: &lastB, PrevClose: &prevB, Timestamp: "2024-06-01T15:30:00Z"},
		})
	}))
	t.Cleanup(server.Close)

	client := &http.Client{
		Transport: rewriteHostTransport{base: http.DefaultTransport, host: server.URL},
		Timeout:   5 * time.Second,
	}
	provider := NewTiingoQuoteProvider(client, func() core.AppSettings {
		return core.AppSettings{TiingoAPIKey: "test-token"}
	})

	quotes, err := provider.Fetch(context.Background(), []core.WatchlistItem{
		{Symbol: "AAPL", Name: "Apple", Market: "US-STOCK", Currency: "USD"},
		{Symbol: "MSFT", Name: "Microsoft", Market: "US-STOCK", Currency: "USD"},
	})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if len(quotes) != 2 {
		t.Fatalf("got %d quotes; want 2", len(quotes))
	}
	if quotes["AAPL"].CurrentPrice != 100 {
		t.Fatalf("AAPL price = %v; want 100", quotes["AAPL"].CurrentPrice)
	}
	if quotes["MSFT"].CurrentPrice != 50.5 {
		t.Fatalf("MSFT price = %v; want 50.5", quotes["MSFT"].CurrentPrice)
	}
}

func TestTiingoHistoryProviderDaily(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/tiingo/daily/") || !strings.HasSuffix(r.URL.Path, "/prices") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("token"); got != "test-token" {
			t.Fatalf("token = %q; want test-token", got)
		}
		adj := 191.0
		_ = json.NewEncoder(w).Encode([]tiingoEODBar{
			{Date: "2024-05-31", Open: 188, High: 192, Low: 187, Close: 190, Volume: 1000, AdjClose: &adj},
			{Date: "2024-06-01", Open: 190, High: 193, Low: 189, Close: 192, Volume: 1100},
		})
	}))
	t.Cleanup(server.Close)

	client := &http.Client{
		Transport: rewriteHostTransport{base: http.DefaultTransport, host: server.URL},
		Timeout:   5 * time.Second,
	}
	provider := NewTiingoHistoryProvider(client, func() core.AppSettings {
		return core.AppSettings{TiingoAPIKey: "test-token"}
	})

	series, err := provider.Fetch(context.Background(), core.WatchlistItem{
		Symbol: "AAPL", Name: "Apple", Market: "US-STOCK", Currency: "USD",
	}, core.HistoryRange1w)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if series.Source != "Tiingo" {
		t.Fatalf("Source = %q; want Tiingo", series.Source)
	}
	if len(series.Points) != 2 {
		t.Fatalf("points = %d; want 2", len(series.Points))
	}
	if series.Points[0].Close != 191 {
		t.Fatalf("first close = %v; want adjClose 191", series.Points[0].Close)
	}
}

func TestTiingoDecodeErrorUsesPlainTextBody(t *testing.T) {
	t.Parallel()

	err := tiingoDecodeError("quote", []byte("You have run over your 500 symbol look up for this month. Please upgrade."), errors.New("boom"))
	if err == nil || !strings.Contains(err.Error(), "You have run over your 500 symbol") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "boom") {
		t.Fatalf("plain-text Tiingo body should replace JSON decode error, got %v", err)
	}
}

func TestTiingoQuoteProviderRequiresAPIKey(t *testing.T) {
	t.Parallel()

	provider := NewTiingoQuoteProvider(nil, func() core.AppSettings { return core.AppSettings{} })
	_, err := provider.Fetch(context.Background(), []core.WatchlistItem{
		{Symbol: "AAPL", Market: "US-STOCK"},
	})
	if err == nil || !strings.Contains(err.Error(), "Tiingo API key is required") {
		t.Fatalf("expected API key error, got %v", err)
	}
}

type rewriteHostTransport struct {
	base http.RoundTripper
	host string
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	target, err := url.Parse(t.host)
	if err != nil {
		return nil, err
	}
	cloned.URL.Scheme = target.Scheme
	cloned.URL.Host = target.Host
	cloned.Host = target.Host
	if t.base == nil {
		t.base = http.DefaultTransport
	}
	return t.base.RoundTrip(cloned)
}
