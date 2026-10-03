package api

import (
	"errors"
	"net/http"

	"investgo/internal/update"
)

// handleUpdateStatus returns the current update state for the settings UI.
func (h *Handler) handleUpdateStatus(writer http.ResponseWriter, request *http.Request) {
	if h.update == nil {
		writeError(writer, request, http.StatusServiceUnavailable, &apiError{message: "Update service is unavailable"})
		return
	}
	writeJSON(writer, http.StatusOK, h.update.Status())
}

// handleUpdateCheck starts an asynchronous update check.
func (h *Handler) handleUpdateCheck(writer http.ResponseWriter, request *http.Request) {
	if h.update == nil || !h.update.Status().Supported {
		writeError(writer, request, http.StatusServiceUnavailable, &apiError{message: "Update service is unavailable"})
		return
	}
	h.update.TriggerCheck()
	writeJSON(writer, http.StatusOK, h.update.Status())
}

// handleUpdateDownload starts downloading the release found by the last check.
func (h *Handler) handleUpdateDownload(writer http.ResponseWriter, request *http.Request) {
	if h.update == nil {
		writeError(writer, request, http.StatusServiceUnavailable, &apiError{message: "Update service is unavailable"})
		return
	}
	if err := h.update.TriggerDownload(); err != nil {
		writeError(writer, request, updateErrorStatus(err), err)
		return
	}
	writeJSON(writer, http.StatusOK, h.update.Status())
}

// handleUpdateRestart installs the downloaded update and restarts the app.
func (h *Handler) handleUpdateRestart(writer http.ResponseWriter, request *http.Request) {
	if h.update == nil {
		writeError(writer, request, http.StatusServiceUnavailable, &apiError{message: "Update service is unavailable"})
		return
	}
	// The service drives the restart with its own lifetime context so a canceled
	// HTTP request cannot abort an install that is already under way.
	if err := h.update.Restart(); err != nil { //nolint:contextcheck
		writeError(writer, request, updateErrorStatus(err), err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
}

// updateErrorStatus maps update service errors onto HTTP statuses.
func updateErrorStatus(err error) int {
	switch {
	case errors.Is(err, update.ErrNotSupported):
		return http.StatusServiceUnavailable
	case errors.Is(err, update.ErrDownloadNotAvailable), errors.Is(err, update.ErrNoPendingUpdate):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
