package api

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseOptionalIntQuery(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/logs?limit=12", nil)
	value, err := parseOptionalIntQuery(request, "limit")
	if err != nil || value != 12 {
		t.Fatalf("parseOptionalIntQuery() = %d, %v; want 12, nil", value, err)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	value, err = parseOptionalIntQuery(request, "limit")
	if err != nil || value != 0 {
		t.Fatalf("empty query = %d, %v; want 0, nil", value, err)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/logs?limit=bad", nil)
	if _, err = parseOptionalIntQuery(request, "limit"); err == nil {
		t.Fatal("invalid query value returned nil error")
	}
}

func TestWriteJSONHandlesMarshalFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusOK, math.NaN())

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("writeJSON() status = %d; want %d", recorder.Code, http.StatusInternalServerError)
	}
}
