package api

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"investgo/internal/core/hot"
	"investgo/internal/core/instrument"
	"investgo/internal/core/pool"
	sqlitestorage "investgo/internal/storage/sqlite"
)

func TestPoolManagementHTTPLifecycle(t *testing.T) {
	t.Parallel()

	handler, ctx, catalog, pools := newPoolAPIHandler(t)
	aapl, err := catalog.Upsert(ctx, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Exchange:   "NASDAQ",
		Symbol:     "AAPL",
		Name:       "Apple",
	})
	if err != nil {
		t.Fatalf("upsert AAPL: %v", err)
	}
	if err := pools.ReplaceBuiltInMembers(ctx, pool.PoolIDUSSP500, "v1", []string{aapl.ID}); err != nil {
		t.Fatalf("seed S&P 500: %v", err)
	}

	response := performPoolRequest(t, handler, http.MethodGet, "/api/pools?page=1&pageSize=1", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /pools status = %d, body = %s", response.Code, response.Body.String())
	}
	var poolPage struct {
		Items   []map[string]any `json:"items"`
		Total   int              `json:"total"`
		HasMore bool             `json:"hasMore"`
	}
	decodePoolResponse(t, response, &poolPage)
	if len(poolPage.Items) != 1 || poolPage.Total != 2 || !poolPage.HasMore {
		t.Fatalf("pool page = %+v", poolPage)
	}

	addPayload := map[string]any{
		"assetClass":    "equity",
		"symbol":        "nvda",
		"name":          "NVIDIA",
		"market":        "US-STOCK",
		"exchange":      "NASDAQ",
		"quoteCurrency": "USD",
	}
	response = performPoolRequest(t, handler, http.MethodPost, "/api/pools/"+pool.PoolIDUSSP500+"/members", addPayload)
	if response.Code != http.StatusCreated {
		t.Fatalf("POST member status = %d, body = %s", response.Code, response.Body.String())
	}
	var added struct {
		Instrument struct {
			ID     string `json:"id"`
			Symbol string `json:"symbol"`
		} `json:"instrument"`
		Source string `json:"source"`
		Status string `json:"status"`
	}
	decodePoolResponse(t, response, &added)
	if added.Instrument.ID == "" || added.Instrument.Symbol != "NVDA" || added.Source != "user" ||
		added.Status != "active" {
		t.Fatalf("added member = %+v", added)
	}

	response = performPoolRequest(t, handler, http.MethodPost, "/api/pools/"+pool.PoolIDUSSP500+"/members", addPayload)
	if response.Code != http.StatusOK {
		t.Fatalf("POST duplicate member status = %d, body = %s", response.Code, response.Body.String())
	}

	response = performPoolRequest(
		t,
		handler,
		http.MethodDelete,
		"/api/pools/"+pool.PoolIDUSSP500+"/members/"+aapl.ID,
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("DELETE built-in member status = %d, body = %s", response.Code, response.Body.String())
	}
	var excluded struct {
		Status string `json:"status"`
		Source string `json:"source"`
	}
	decodePoolResponse(t, response, &excluded)
	if excluded.Status != "excluded" || excluded.Source != "builtin" {
		t.Fatalf("excluded member = %+v", excluded)
	}

	response = performPoolRequest(
		t,
		handler,
		http.MethodGet,
		"/api/pools/"+pool.PoolIDUSSP500+"/members?status=excluded",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("GET excluded members status = %d, body = %s", response.Code, response.Body.String())
	}
	var excludedPage struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	decodePoolResponse(t, response, &excludedPage)
	if len(excludedPage.Items) != 1 || excludedPage.Total != 1 {
		t.Fatalf("excluded page = %+v", excludedPage)
	}

	response = performPoolRequest(
		t,
		handler,
		http.MethodPost,
		"/api/pools/"+pool.PoolIDUSSP500+"/members/"+aapl.ID+"/restore",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("POST restore status = %d, body = %s", response.Code, response.Body.String())
	}

	response = performPoolRequest(
		t,
		handler,
		http.MethodDelete,
		"/api/pools/"+pool.PoolIDUSSP500+"/members/"+added.Instrument.ID,
		nil,
	)
	if response.Code != http.StatusNoContent {
		t.Fatalf("DELETE user member status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, found, err := pools.GetOverride(ctx, pool.PoolIDUSSP500, added.Instrument.ID); err != nil || found {
		t.Fatalf("user override after delete = found %v, error %v", found, err)
	}
}

func TestPoolManagementHTTPValidationAndNotFound(t *testing.T) {
	t.Parallel()

	handler, _, _, _ := newPoolAPIHandler(t)
	response := performPoolRequest(
		t,
		handler,
		http.MethodPost,
		"/api/pools/"+pool.PoolIDUSSP500+"/members",
		map[string]any{
			"assetClass": "equity",
			"symbol":     "***",
			"name":       "Invalid",
			"market":     "US-STOCK",
		},
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid member status = %d, body = %s", response.Code, response.Body.String())
	}

	response = performPoolRequest(t, handler, http.MethodGet, "/api/pools/missing/members", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing pool status = %d, body = %s", response.Code, response.Body.String())
	}

	response = performPoolRequest(t, handler, http.MethodGet, "/api/pools?pageSize=1000", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized page status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestPoolManagementHTTPEditMember(t *testing.T) {
	t.Parallel()

	handler, ctx, catalog, pools := newPoolAPIHandler(t)
	aapl, err := catalog.Upsert(ctx, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Exchange:   "NASDAQ",
		Symbol:     "AAPL",
		Name:       "Apple",
	})
	if err != nil {
		t.Fatalf("upsert AAPL: %v", err)
	}
	if err := pools.ReplaceBuiltInMembers(ctx, pool.PoolIDUSSP500, "v1", []string{aapl.ID}); err != nil {
		t.Fatalf("seed S&P 500: %v", err)
	}

	// Edit a built-in member's name.
	response := performPoolRequest(
		t,
		handler,
		http.MethodPut,
		"/api/pools/"+pool.PoolIDUSSP500+"/members/"+aapl.ID,
		map[string]any{
			"name": "Apple Inc.",
		},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT edit built-in status = %d, body = %s", response.Code, response.Body.String())
	}
	var edited struct {
		Instrument struct {
			ID     string `json:"id"`
			Symbol string `json:"symbol"`
			Name   string `json:"name"`
		} `json:"instrument"`
		Source string `json:"source"`
	}
	decodePoolResponse(t, response, &edited)
	if edited.Instrument.Name != "Apple Inc." || edited.Instrument.ID != aapl.ID || edited.Source != "builtin" {
		t.Fatalf("edited built-in member = %+v", edited)
	}

	// Editing to another member's symbol is rejected.
	msft, err := catalog.Upsert(ctx, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Exchange:   "NASDAQ",
		Symbol:     "MSFT",
		Name:       "Microsoft",
	})
	if err != nil {
		t.Fatalf("upsert MSFT: %v", err)
	}
	if err := pools.ReplaceBuiltInMembers(ctx, pool.PoolIDUSSP500, "v2", []string{aapl.ID, msft.ID}); err != nil {
		t.Fatalf("seed S&P 500 v2: %v", err)
	}
	response = performPoolRequest(
		t,
		handler,
		http.MethodPut,
		"/api/pools/"+pool.PoolIDUSSP500+"/members/"+aapl.ID,
		map[string]any{
			"symbol": "MSFT",
		},
	)
	if response.Code != http.StatusConflict {
		t.Fatalf("PUT collision status = %d, body = %s", response.Code, response.Body.String())
	}

	// Symbol rename persists in the effective membership.
	response = performPoolRequest(
		t,
		handler,
		http.MethodPut,
		"/api/pools/"+pool.PoolIDUSSP500+"/members/"+aapl.ID,
		map[string]any{
			"symbol": "META",
		},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT rename status = %d, body = %s", response.Code, response.Body.String())
	}
	decodePoolResponse(t, response, &edited)
	if edited.Instrument.Symbol != "META" || edited.Instrument.Name != "Apple Inc." {
		t.Fatalf("renamed built-in member = %+v", edited)
	}
}

func newPoolAPIHandler(t *testing.T) (*Handler, context.Context, instrument.Repository, pool.Repository) {
	t.Helper()

	ctx := context.Background()
	db, err := sqlitestorage.Open(filepath.Join(t.TempDir(), "investgo.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	catalog := sqlitestorage.NewInstrumentRepository(db)
	pools := sqlitestorage.NewPoolRepository(db)
	for _, definition := range []pool.Pool{
		{ID: pool.PoolIDUSSP500, Name: "S&P 500", Market: "US-STOCK", AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex},
		{ID: pool.PoolIDUSNasdaq, Name: "Nasdaq 100", Market: "US-STOCK", AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex},
	} {
		if _, err := pools.Upsert(ctx, definition); err != nil {
			t.Fatalf("upsert pool %s: %v", definition.ID, err)
		}
	}
	poolService := pool.NewService(catalog, pools)
	hotService := hot.NewHotService(nil, nil, nil, poolService)
	return NewHandler(nil, hotService, nil, nil, poolService), ctx, catalog, pools
}

func performPoolRequest(
	t *testing.T,
	handler http.Handler,
	method, target string,
	payload any,
) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	if payload != nil {
		if err := json.MarshalWrite(&body, payload); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}
	request := httptest.NewRequest(method, target, &body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodePoolResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}
