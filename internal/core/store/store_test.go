package store

import (
	"io"
	"net/http"
	"strings"
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
