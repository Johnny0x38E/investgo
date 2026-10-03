package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"investgo/internal/core/pool"
)

type statusClass int

const (
	classValidation statusClass = iota
	classNotFound
	classUpstream
	classUnavailable
	classInternal
	classConflict
)

// statusForError maps handler failures onto a consistent HTTP status.
// Validation and other caller mistakes are 400, missing records are 404,
// upstream or provider failures are 502, and a missing dependency or a
// canceled/timed-out call is 503. Pool conflicts stay 409, matching writePoolError.
func statusForError(err error) int {
	if err == nil {
		return http.StatusInternalServerError
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return http.StatusServiceUnavailable
	}
	if errors.Is(err, pool.ErrInvalidOperation) {
		return http.StatusConflict
	}
	if errors.Is(err, pool.ErrPoolNotFound) || errors.Is(err, pool.ErrInstrumentNotFound) ||
		errors.Is(err, pool.ErrMemberNotFound) {
		return http.StatusNotFound
	}

	sawNotFound := false
	sawUpstream := false
	sawUnavailable := false
	sawInternal := false
	for _, part := range splitStatusMessages(err.Error()) {
		switch classifyStatusMessage(part) {
		case classNotFound:
			sawNotFound = true
		case classUpstream:
			sawUpstream = true
		case classUnavailable:
			sawUnavailable = true
		case classInternal:
			sawInternal = true
		case classConflict:
			return http.StatusConflict
		}
	}
	switch {
	case sawInternal:
		return http.StatusInternalServerError
	case sawUpstream:
		return http.StatusBadGateway
	case sawUnavailable:
		return http.StatusServiceUnavailable
	case sawNotFound:
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

func writeClassifiedError(writer http.ResponseWriter, request *http.Request, err error) {
	writeError(writer, request, statusForError(err), err)
}

func splitStatusMessages(message string) []string {
	normalized := strings.ReplaceAll(message, "；", ";")
	parts := strings.Split(normalized, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if candidate := strings.TrimSpace(part); candidate != "" {
			out = append(out, candidate)
		}
	}
	if len(out) == 0 {
		return []string{strings.TrimSpace(message)}
	}
	return out
}

func classifyStatusMessage(message string) statusClass {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.HasPrefix(message, "Item not found:"),
		strings.HasPrefix(message, "Alert item not found:"),
		strings.HasPrefix(message, "Alert not found:"),
		strings.HasPrefix(lower, "pool not found:"),
		strings.HasPrefix(lower, "instrument not found:"),
		strings.HasPrefix(message, "Pool member not found:"):
		return classNotFound
	case message == "History provider is not configured",
		message == "Hot service is unavailable",
		message == "Pool service is unavailable",
		message == "Hot quote provider is not configured",
		message == "pool service is not configured",
		message == "state repository is not configured":
		return classUnavailable
	case strings.Contains(lower, "save state failed"),
		strings.Contains(lower, "save sqlite"),
		strings.Contains(lower, "database is nil"):
		return classInternal
	case isUpstreamStatusMessage(lower):
		return classUpstream
	default:
		return classValidation
	}
}

func isUpstreamStatusMessage(lower string) bool {
	needles := []string{
		"request failed",
		"providers failed",
		"quote response",
		"history response",
		"service is unreachable",
		"service returned",
		"no live hot quotes",
		"fallback quote",
		"upstream",
		"rc=",
		"unexpected status",
	}
	for _, needle := range needles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
