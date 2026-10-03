package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"investgo/internal/update"
)

func TestUpdateEndpointsUnavailableWithoutService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil)

	response := performAPIRequest(t, handler, http.MethodGet, "/api/update/status", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}

	response = performAPIRequest(t, handler, http.MethodPost, "/api/update/check", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("check status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestUpdateStatusReportsUnsupportedBuild(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil)
	handler.SetUpdateService(update.New(update.Options{}))

	response := performAPIRequest(t, handler, http.MethodGet, "/api/update/status", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var status update.Status
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Supported {
		t.Fatal("supported = true without an attached updater")
	}

	response = performAPIRequest(t, handler, http.MethodPost, "/api/update/check", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("check status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}
