package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"investgo/internal/core"
)

type gateQuoteProvider struct {
	mu      sync.Mutex
	arrived int
	ready   chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *gateQuoteProvider) Name() string { return "Sina Finance" }

func (p *gateQuoteProvider) Fetch(ctx context.Context, _ []core.WatchlistItem) (map[string]core.Quote, error) {
	p.mu.Lock()
	p.arrived++
	arrived := p.arrived
	p.mu.Unlock()
	if arrived >= 2 {
		p.once.Do(func() { close(p.ready) })
	}
	select {
	case <-p.release:
		return map[string]core.Quote{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestUpsertItemRejectsConcurrentDuplicate(t *testing.T) {
	provider := &gateQuoteProvider{
		ready:   make(chan struct{}),
		release: make(chan struct{}),
	}
	appStore := newLookupTestStore(t, &lookupStubProvider{name: "Sina Finance"})
	appStore.quoteProviders["sina"] = provider

	item := core.WatchlistItem{
		Symbol:   "600519",
		Name:     "贵州茅台",
		Market:   "CN-A",
		Currency: "CNY",
	}
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := appStore.UpsertItem(context.Background(), item)
			errCh <- err
		}()
	}

	select {
	case <-provider.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("both creates did not reach the quote fetch")
	}
	close(provider.release)
	wg.Wait()
	close(errCh)

	failures := 0
	for err := range errCh {
		if err == nil {
			continue
		}
		failures++
		if !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("duplicate error = %v", err)
		}
	}
	if failures != 1 {
		t.Fatalf("duplicate failures = %d, want 1", failures)
	}
	if got := len(appStore.Snapshot().Items); got != 1 {
		t.Fatalf("items = %d, want 1", got)
	}
}

type cancelQuoteProvider struct {
	seen atomic.Value
}

func (p *cancelQuoteProvider) Name() string { return "Sina Finance" }

func (p *cancelQuoteProvider) Fetch(ctx context.Context, _ []core.WatchlistItem) (map[string]core.Quote, error) {
	select {
	case <-ctx.Done():
		p.seen.Store(ctx.Err())
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		p.seen.Store(errors.New("quote fetch ignored the request context"))
		return map[string]core.Quote{}, nil
	}
}

func TestUpsertItemQuoteFetchUsesCallerContext(t *testing.T) {
	provider := &cancelQuoteProvider{}
	appStore := newLookupTestStore(t, &lookupStubProvider{name: "Sina Finance"})
	appStore.quoteProviders["sina"] = provider

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := appStore.UpsertItem(ctx, core.WatchlistItem{
		Symbol:   "600519",
		Name:     "贵州茅台",
		Market:   "CN-A",
		Currency: "CNY",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	seen, _ := provider.seen.Load().(error)
	if !errors.Is(seen, context.Canceled) {
		t.Fatalf("quote fetch context error = %v, want context.Canceled", seen)
	}
}
