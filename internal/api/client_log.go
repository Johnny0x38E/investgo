package api

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strings"

	"investgo/internal/logger"
)

const (
	maxClientLogBodyBytes    = 32 * 1024
	maxClientLogBatch        = 20
	maxClientLogMessageRunes = 4000
	maxClientLogFieldRunes   = 64
)

// readClientLogs decodes one log object or a JSON array of them.
// The body is capped, each message is redacted, and over-long text is truncated.
func readClientLogs(writer http.ResponseWriter, request *http.Request) ([]clientLogRequest, error) {
	request.Body = http.MaxBytesReader(writer, request.Body, maxClientLogBodyBytes)
	defer request.Body.Close() // nolint:errcheck

	payload, err := io.ReadAll(request.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, &apiError{message: "Client log payload is too large"}
		}
		return nil, &apiError{message: "Invalid JSON request body"}
	}

	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil, &apiError{message: "Invalid JSON request body"}
	}

	if trimmed[0] == '[' {
		var entries []clientLogRequest
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			return nil, &apiError{message: "Invalid JSON request body"}
		}
		if len(entries) > maxClientLogBatch {
			return nil, &apiError{message: "Client log batch is too large"}
		}
		return entries, nil
	}

	var entry clientLogRequest
	if err := json.Unmarshal(trimmed, &entry); err != nil {
		return nil, &apiError{message: "Invalid JSON request body"}
	}
	return []clientLogRequest{entry}, nil
}

func (h *Handler) recordClientLog(entry clientLogRequest) {
	if h.logs == nil {
		return
	}
	message := truncateRunes(strings.TrimSpace(logger.RedactSensitiveText(entry.Message)), maxClientLogMessageRunes)
	if message == "" {
		return
	}
	source := truncateRunes(strings.TrimSpace(entry.Source), maxClientLogFieldRunes)
	scope := truncateRunes(strings.TrimSpace(entry.Scope), maxClientLogFieldRunes)
	h.logs.Log(source, scope, sanitiseDeveloperLogLevel(entry.Level), message)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}
