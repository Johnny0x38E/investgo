package fx

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchWaitsForInFlightRequestAndReusesSuccessfulRates(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32

	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-release
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`{"base":"CNY","date":"2026-08-01","rates":{"USD":0.14}}`,
			)),
			Header: make(http.Header),
		}, nil
	})}

	rates := NewFxRates(client)
	firstDone := make(chan error, 1)
	go func() { firstDone <- rates.Fetch(context.Background()) }()
	<-started

	secondDone := make(chan error, 1)
	go func() { secondDone <- rates.Fetch(context.Background()) }()

	select {
	case err := <-secondDone:
		t.Fatalf("second fetch returned before first completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one upstream request, got %d", calls.Load())
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
