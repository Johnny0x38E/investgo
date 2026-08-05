package store

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"investgo/internal/core"
	"investgo/internal/logger"
)

func TestInitialFXFetchStartsOnlyAfterExplicitStart(t *testing.T) {
	requests := make(chan struct{}, 2)
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests <- struct{}{}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`{"base":"CNY","date":"2026-08-01","rates":{"USD":0.14}}`,
			)),
			Header: make(http.Header),
		}, nil
	})}

	appStore, err := NewStoreWithRepository(
		&memoryRepository{},
		nil,
		nil,
		nil,
		logger.NewLogBook(10),
		"test",
		client,
	)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	select {
	case <-requests:
		t.Fatal("initial FX request started before transport configuration")
	case <-time.After(50 * time.Millisecond):
	}

	appStore.StartInitialFXFetch()
	appStore.StartInitialFXFetch()
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("initial FX request did not start")
	}

	select {
	case <-requests:
		t.Fatal("initial FX request started more than once")
	case <-time.After(50 * time.Millisecond):
	}
}

type memoryRepository struct{}

func (*memoryRepository) Load(target any) (bool, error) {
	state := target.(*persistedState)
	*state = persistedState{Settings: core.AppSettings{ProxyMode: "system"}}
	return true, nil
}

func (*memoryRepository) Save(any) error { return nil }

func (*memoryRepository) Path() string { return "memory" }

func TestFailedRefreshIsNotCached(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("FX service unavailable")
	})}

	appStore, err := NewStoreWithRepository(
		&seededMemoryRepository{state: persistedState{
			Items: []core.WatchlistItem{
				{
					ID:           "item-us",
					Symbol:       "VOO",
					Name:         "VOO",
					Market:       "US-ETF",
					Currency:     "USD",
					Quantity:     1,
					CostPrice:    1,
					CurrentPrice: 10,
				},
			},
			Settings: core.AppSettings{
				HotCacheTTLSeconds: 60,
				ProxyMode:          "system",
				DashboardCurrency:  "CNY",
			},
			UpdatedAt: time.Now(),
		}},
		nil,
		nil,
		nil,
		logger.NewLogBook(10),
		"test",
		client,
	)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	first, err := appStore.Refresh(context.Background(), true)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if first.Runtime.LastFxError == "" {
		t.Fatal("expected first refresh to expose FX failure")
	}

	second, err := appStore.Refresh(context.Background(), false)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if second.Runtime.LastFxError == "" {
		t.Fatal("expected second refresh to retry and expose FX failure")
	}
	if requests.Load() < 2 {
		t.Fatalf("expected failed refresh to retry upstream, got %d request(s)", requests.Load())
	}
}

func TestInitialFXFetchInvalidatesSnapshotCache(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`{"base":"CNY","date":"2026-08-01","rates":{"USD":0.14}}`,
			)),
			Header: make(http.Header),
		}, nil
	})}

	appStore, err := NewStoreWithRepository(
		&seededMemoryRepository{state: persistedState{
			Items: []core.WatchlistItem{
				{
					ID:           "item-us",
					Symbol:       "VOO",
					Name:         "VOO",
					Market:       "US-ETF",
					Currency:     "USD",
					Quantity:     1,
					CostPrice:    1,
					CurrentPrice: 10,
				},
			},
			Settings: core.AppSettings{
				HotCacheTTLSeconds: 60,
				ProxyMode:          "system",
				DashboardCurrency:  "CNY",
			},
			UpdatedAt: time.Now(),
		}},
		nil,
		nil,
		nil,
		logger.NewLogBook(10),
		"test",
		client,
	)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	before := appStore.Snapshot()
	if before.Dashboard.TotalValue != 10 {
		t.Fatalf("expected uncached USD value before FX fetch, got %v", before.Dashboard.TotalValue)
	}

	appStore.StartInitialFXFetch()
	deadline := time.After(time.Second)
	for appStore.snapshotCache.Load() != nil {
		select {
		case <-deadline:
			t.Fatal("initial FX fetch did not invalidate the snapshot cache")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	after := appStore.Snapshot()
	if after.Runtime.LastFxRefreshAt == nil {
		t.Fatal("snapshot cache still contains stale runtime FX metadata")
	}
	if after.Dashboard.TotalValue <= before.Dashboard.TotalValue {
		t.Fatalf("expected FX-converted dashboard value after fetch, got %v", after.Dashboard.TotalValue)
	}
}

type seededMemoryRepository struct {
	state persistedState
}

func (r *seededMemoryRepository) Load(target any) (bool, error) {
	*target.(*persistedState) = r.state
	return true, nil
}

func (*seededMemoryRepository) Save(any) error { return nil }

func (*seededMemoryRepository) Path() string { return "seeded-memory" }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
