package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"investgo/internal/logger"
)

func TestClientLogsRedactsAndTruncates(t *testing.T) {
	logs := logger.NewLogBook(10)
	handler := NewHandler(nil, nil, logs, nil)

	body := `{"source":"frontend","scope":"console","level":"info","message":"alphaVantageApiKey=supersecret"}`
	response := performAPIRequest(t, handler, http.MethodPost, "/api/client-logs", body)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	snapshot := logs.Snapshot(10)
	if len(snapshot.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(snapshot.Entries))
	}
	if strings.Contains(snapshot.Entries[0].Message, "supersecret") {
		t.Fatalf("log kept secret: %s", snapshot.Entries[0].Message)
	}
	if !strings.Contains(snapshot.Entries[0].Message, "alphaVantageApiKey=***") {
		t.Fatalf("log = %s", snapshot.Entries[0].Message)
	}
}

func TestClientLogsRejectsOversizedPayloadAndBatch(t *testing.T) {
	logs := logger.NewLogBook(10)
	handler := NewHandler(nil, nil, logs, nil)

	oversized := `{"source":"frontend","scope":"console","level":"info","message":"` + strings.Repeat("a", maxClientLogBodyBytes) + `"}`
	response := performAPIRequest(t, handler, http.MethodPost, "/api/client-logs", oversized)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "too large") {
		t.Fatalf("oversize status = %d, body = %s", response.Code, response.Body.String())
	}

	entries := make([]string, maxClientLogBatch+1)
	for i := range entries {
		entries[i] = `{"source":"frontend","scope":"console","level":"info","message":"ok"}`
	}
	response = performAPIRequest(t, handler, http.MethodPost, "/api/client-logs", "["+strings.Join(entries, ",")+"]")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "batch") {
		t.Fatalf("batch status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := len(logs.Snapshot(10).Entries); got != 0 {
		t.Fatalf("rejected batch wrote %d logs", got)
	}
}

func TestClientLogsTruncatesLongMessage(t *testing.T) {
	logs := logger.NewLogBook(10)
	handler := NewHandler(nil, nil, logs, nil)
	message := strings.Repeat("字", maxClientLogMessageRunes+25)
	body := `{"source":"frontend-source-that-is-longer-than-the-field-limit-and-should-be-cut","scope":"console","level":"warn","message":"` + message + `"}`
	response := performAPIRequest(t, handler, http.MethodPost, "/api/client-logs", body)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	entry := logs.Snapshot(10).Entries[0]
	if got := len([]rune(entry.Message)); got != maxClientLogMessageRunes {
		t.Fatalf("message runes = %d, want %d", got, maxClientLogMessageRunes)
	}
	if !strings.HasSuffix(entry.Message, "…") {
		t.Fatalf("message was not truncated: %s", entry.Message)
	}
	if got := len([]rune(entry.Source)); got > maxClientLogFieldRunes {
		t.Fatalf("source runes = %d, want <= %d", got, maxClientLogFieldRunes)
	}
}

func performAPIRequest(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
