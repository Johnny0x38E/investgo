package fx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const frankfurterAPI = "https://api.frankfurter.dev/v1/latest"

// FxRates caches FX rates of various currencies against CNY for dashboard multi-currency aggregation.
// All rates are stored with the benchmark "1 unit of foreign currency = X CNY".
// Uses Frankfurter API (European Central Bank data), cached for at least 2 hours.
type FxRates struct {
	mu        sync.RWMutex
	rates     map[string]float64 // foreign currency -> CNY
	validAt   time.Time
	lastError string // error message from the most recent fetch failure
	client    *http.Client
	fetching  bool
	fetchDone chan struct{}
}

// NewFxRates creates FX rate service, initialized with only CNY=1.0.
func NewFxRates(client *http.Client) *FxRates {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &FxRates{
		client: client,
		rates:  map[string]float64{"CNY": 1.0},
	}
}

// NewFxRatesWithRates creates an FxRates instance pre-loaded with a specific rate map.
// Intended for use in tests that need deterministic conversion results without network calls.
func NewFxRatesWithRates(rates map[string]float64) *FxRates {
	if rates == nil {
		rates = map[string]float64{"CNY": 1.0}
	}
	return &FxRates{
		client: &http.Client{Timeout: 10 * time.Second},
		rates:  rates,
	}
}

// IsStale returns whether FX rate cache has exceeded 2 hours.
func (f *FxRates) IsStale() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.isStaleLocked()
}

func (f *FxRates) isStaleLocked() bool {
	return f.validAt.IsZero() || time.Since(f.validAt) > 2*time.Hour
}

// LastError returns the error message from the most recent fetch failure; empty on success.
func (f *FxRates) LastError() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastError
}

// ValidAt returns the time of the most recent successful FX rate fetch.
func (f *FxRates) ValidAt() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.validAt
}

// CurrencyCount returns the number of currencies currently cached.
func (f *FxRates) CurrencyCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.rates)
}

// frankfurterResponse is the structure of the Frankfurter API response.
type frankfurterResponse struct {
	Base  string             `json:"base"`
	Date  string             `json:"date"`
	Rates map[string]float64 `json:"rates"`
}

// Fetch fetches FX rates of various currencies against CNY from the Frankfurter API.
// Fetches foreign currency rates with CNY as base, then takes reciprocals to get "foreign currency → CNY" mapping.
// Concurrent callers share one in-flight request and receive the same success or error result.
func (f *FxRates) Fetch(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		f.mu.Lock()
		if !f.isStaleLocked() {
			f.mu.Unlock()
			return nil
		}
		if f.fetching {
			done := f.fetchDone
			f.mu.Unlock()

			select {
			case <-done:
				f.mu.RLock()
				fresh := !f.isStaleLocked()
				lastError := f.lastError
				f.mu.RUnlock()

				if fresh {
					return nil
				}
				if lastError != "" {
					return errors.New(lastError)
				}
				// The completed caller should always record either fresh rates or an
				// error. Loop defensively if a future implementation changes that.
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		f.fetching = true
		f.fetchDone = make(chan struct{})
		done := f.fetchDone
		f.mu.Unlock()

		err := f.fetchOnce(ctx)

		f.mu.Lock()
		f.fetching = false
		close(done)
		f.mu.Unlock()

		return err
	}
}

func (f *FxRates) fetchOnce(ctx context.Context) error {
	url := frankfurterAPI + "?from=CNY"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		message := fmt.Sprintf("Failed to create FX request: %v", err)
		f.setError(message)
		return errors.New(message)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		message := fmt.Sprintf("FX service is unreachable: %v", err)
		f.setError(message)
		return errors.New(message)
	}
	defer resp.Body.Close() // nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		message := fmt.Sprintf("Failed to read FX response: %v", err)
		f.setError(message)
		return errors.New(message)
	}

	if resp.StatusCode != http.StatusOK {
		detail := string(body)
		if len(detail) > 200 {
			detail = detail[:200]
		}
		message := fmt.Sprintf("FX service returned %d: %s", resp.StatusCode, detail)
		f.setError(message)
		return errors.New(message)
	}

	var data frankfurterResponse
	if err := json.Unmarshal(body, &data); err != nil {
		message := fmt.Sprintf("Failed to decode FX data: %v", err)
		f.setError(message)
		return errors.New(message)
	}
	if data.Base != "CNY" || len(data.Rates) == 0 {
		message := "FX payload is invalid"
		f.setError(message)
		return errors.New(message)
	}

	newRates := make(map[string]float64, len(data.Rates)+1)
	newRates["CNY"] = 1.0
	for currency, rate := range data.Rates {
		if rate > 0 {
			// Frankfurter returns rates as "1 CNY = X <currency>"; invert to store as "1 <currency> = X CNY".
			newRates[currency] = 1.0 / rate
		}
	}

	f.mu.Lock()
	f.rates = newRates
	f.validAt = time.Now()
	f.lastError = ""
	f.mu.Unlock()
	return nil
}

func (f *FxRates) setError(msg string) {
	f.mu.Lock()
	f.lastError = msg
	f.mu.Unlock()
}

// Convert converts a given amount from source currency to target currency, using CNY as the intermediate currency.
// Returns the original value if currencies are the same or cannot be resolved.
func (f *FxRates) Convert(value float64, from, to string) float64 {
	from = strings.ToUpper(strings.TrimSpace(from))
	to = strings.ToUpper(strings.TrimSpace(to))
	if from == to || value == 0 {
		return value
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	fromRate, ok := f.rates[from]
	if !ok || fromRate <= 0 {
		return value
	}
	cnyValue := value * fromRate

	if to == "CNY" {
		return cnyValue
	}
	toRate, ok := f.rates[to]
	if !ok || toRate <= 0 {
		return cnyValue
	}
	return cnyValue / toRate
}
