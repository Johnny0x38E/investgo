package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"investgo/internal/core"
	"investgo/internal/core/hot"
	"investgo/internal/core/instrument"
	"investgo/internal/core/pool"
	"investgo/internal/core/store"
	sqlitestorage "investgo/internal/storage/sqlite"
)

type memoryStateRepository struct {
	mu    sync.Mutex
	state store.PersistedState
	err   error
}

func (r *memoryStateRepository) Load() (store.PersistedState, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, true, nil
}

func (r *memoryStateRepository) Save(state store.PersistedState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.state = state
	return nil
}

func (r *memoryStateRepository) Path() string { return "memory" }

type failingHistoryProvider struct{}

func (failingHistoryProvider) Name() string { return "Yahoo Finance" }

func (failingHistoryProvider) Fetch(
	context.Context,
	core.WatchlistItem,
	core.HistoryInterval,
) (core.HistorySeries, error) {
	return core.HistorySeries{}, errors.New("Yahoo quote request failed: status 502")
}

func TestHTTPStatusCodesForStoreFailures(t *testing.T) {
	repo := &memoryStateRepository{state: store.PersistedState{
		Items: []core.WatchlistItem{{
			ID:           "item-voo",
			Symbol:       "VOO",
			Name:         "VOO",
			Market:       "US-ETF",
			Currency:     "USD",
			Quantity:     1,
			CostPrice:    1,
			CurrentPrice: 10,
		}},
		Settings: storeSettings(),
	}}
	appStore, err := store.NewStoreWithRepository(repo, nil, nil, failingHistoryProvider{}, nil, "test", nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	handler := NewHandler(appStore, nil, nil, nil)

	missing := performAPIRequest(t, handler, http.MethodDelete, "/api/items/missing", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing status = %d, body = %s", missing.Code, missing.Body.String())
	}

	history := performAPIRequest(t, handler, http.MethodGet, "/api/history?itemId=missing", "")
	if history.Code != http.StatusNotFound {
		t.Fatalf("history missing status = %d, body = %s", history.Code, history.Body.String())
	}

	unconfigured := NewHandler(mustStore(t, &memoryStateRepository{state: store.PersistedState{
		Items:    []core.WatchlistItem{{ID: "item-voo", Symbol: "VOO", Name: "VOO", Market: "US-ETF", Currency: "USD"}},
		Settings: storeSettings(),
	}}), nil, nil, nil)
	unavailable := performAPIRequest(t, unconfigured, http.MethodGet, "/api/history?itemId=item-voo", "")
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("history unconfigured status = %d, body = %s", unavailable.Code, unavailable.Body.String())
	}

	overview := performAPIRequest(t, handler, http.MethodGet, "/api/overview", "")
	if overview.Code != http.StatusBadGateway {
		t.Fatalf("overview upstream status = %d, body = %s", overview.Code, overview.Body.String())
	}
}

func TestSettingsPartialUpdateOverHTTP(t *testing.T) {
	repo := &memoryStateRepository{state: store.PersistedState{Settings: storeSettings()}}
	appStore := mustStore(t, repo)
	secret := "secret-key"
	developerMode := true
	if _, err := appStore.UpdateSettings(store.SettingsUpdate{
		AlphaVantageAPIKey: &secret,
		DeveloperMode:      &developerMode,
	}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	handler := NewHandler(appStore, nil, nil, nil)
	response := performAPIRequest(t, handler, http.MethodPut, "/api/settings", `{"themeMode":"dark"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT settings status = %d, body = %s", response.Code, response.Body.String())
	}
	settings := appStore.CurrentSettings()
	if settings.ThemeMode != "dark" || settings.AlphaVantageAPIKey != secret || !settings.DeveloperMode {
		t.Fatalf("settings = %+v", settings)
	}
}

type contextQuoteProvider struct {
	mu   sync.Mutex
	seen error
}

func (p *contextQuoteProvider) Name() string { return "Sina Finance" }

func (p *contextQuoteProvider) Fetch(ctx context.Context, _ []core.WatchlistItem) (map[string]core.Quote, error) {
	select {
	case <-ctx.Done():
		p.mu.Lock()
		p.seen = ctx.Err()
		p.mu.Unlock()
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		p.mu.Lock()
		p.seen = errors.New("quote fetch ignored the request context")
		p.mu.Unlock()
		return map[string]core.Quote{}, nil
	}
}

func TestCreateItemUsesRequestContext(t *testing.T) {
	provider := &contextQuoteProvider{}
	appStore, err := store.NewStoreWithRepository(
		&memoryStateRepository{state: store.PersistedState{Settings: storeSettings()}},
		map[string]core.QuoteProvider{"sina": provider, "yahoo": provider, "xueqiu": provider},
		[]core.QuoteSourceOption{
			{ID: "sina", Name: "Sina Finance"},
			{ID: "yahoo", Name: "Yahoo Finance"},
			{ID: "xueqiu", Name: "Xueqiu"},
		},
		nil,
		nil,
		"test",
		nil,
	)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	handler := NewHandler(appStore, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/items",
		strings.NewReader(`{"symbol":"600519","name":"贵州茅台","market":"CN-A","currency":"CNY"}`),
	)
	request = request.WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if !errors.Is(provider.seen, context.Canceled) {
		t.Fatalf("provider context error = %v, want context.Canceled", provider.seen)
	}
}

func TestUpdatePoolMemberSurfacesDisplaySyncFailure(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitestorage.Open(t.TempDir() + "/investgo.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	catalog := sqlitestorage.NewInstrumentRepository(db)
	pools := sqlitestorage.NewPoolRepository(db)
	if _, err := pools.Upsert(ctx, pool.Pool{
		ID:         pool.PoolIDCNA,
		Name:       "A-share",
		Market:     "CN-A",
		AssetClass: instrument.AssetClassEquity,
		Type:       pool.TypeBuiltIn,
	}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	poolService := pool.NewService(catalog, pools)
	repo := &memoryStateRepository{
		state: store.PersistedState{
			Items: []core.WatchlistItem{{
				ID:       "item-moutai",
				Symbol:   "600519",
				Name:     "贵州茅台",
				Market:   "CN-A",
				Currency: "CNY",
			}},
			Settings: storeSettings(),
		},
		err: errors.New("disk full"),
	}
	appStore := mustStore(t, repo)
	handler := NewHandler(appStore, hot.NewHotService(nil, nil, nil, poolService), nil, nil, poolService)

	created := performAPIRequest(
		t,
		handler,
		http.MethodPost,
		"/api/pools/"+pool.PoolIDCNA+"/members",
		`{"assetClass":"equity","symbol":"600519","name":"贵州茅台","market":"CN-A","quoteCurrency":"CNY"}`,
	)
	if created.Code != http.StatusCreated && created.Code != http.StatusOK {
		t.Fatalf("add member status = %d, body = %s", created.Code, created.Body.String())
	}
	var member struct {
		Instrument struct {
			ID string `json:"id"`
		} `json:"instrument"`
	}
	decodePoolResponse(t, created, &member)
	if member.Instrument.ID == "" {
		t.Fatal("added member id is empty")
	}

	updated := performAPIRequest(
		t,
		handler,
		http.MethodPut,
		"/api/pools/"+pool.PoolIDCNA+"/members/"+member.Instrument.ID,
		`{"name":"茅台股份"}`,
	)
	if updated.Code != http.StatusInternalServerError ||
		!strings.Contains(updated.Body.String(), "failed to sync instrument display name") {
		t.Fatalf("display sync status = %d, body = %s", updated.Code, updated.Body.String())
	}
}

func storeSettings() core.AppSettings {
	return core.AppSettings{
		HotCacheTTLSeconds: 60,
		CNQuoteSource:      "sina",
		HKQuoteSource:      "xueqiu",
		USQuoteSource:      "yahoo",
		ThemeMode:          "system",
		ColorTheme:         "blue",
		FontPreset:         "system",
		AmountDisplay:      "full",
		CurrencyDisplay:    "symbol",
		PriceColorScheme:   "cn",
		Locale:             "system",
		ProxyMode:          "system",
		DashboardCurrency:  "CNY",
	}
}

func mustStore(t *testing.T, repo store.Repository) *store.Store {
	t.Helper()
	appStore, err := store.NewStoreWithRepository(repo, nil, nil, nil, nil, "test", nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return appStore
}
