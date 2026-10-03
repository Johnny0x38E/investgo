package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"investgo/internal/core/pool"
)

func TestStatusForError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "validation", err: errors.New("Quantity must not be negative"), want: http.StatusBadRequest},
		{
			name: "pool status filter",
			err:  errors.New("Pool member status must be active or excluded"),
			want: http.StatusBadRequest,
		},
		{name: "item missing", err: errors.New("Item not found: item-1"), want: http.StatusNotFound},
		{name: "alert missing", err: errors.New("Alert not found: alert-1"), want: http.StatusNotFound},
		{name: "pool member missing", err: pool.ErrMemberNotFound, want: http.StatusNotFound},
		{name: "pool conflict", err: pool.ErrInvalidOperation, want: http.StatusConflict},
		{
			name: "history upstream",
			err:  errors.New("VOO: Yahoo quote request failed: status 502"),
			want: http.StatusBadGateway,
		},
		{
			name: "hot upstream",
			err:  errors.New("EastMoney hot request failed: status 500"),
			want: http.StatusBadGateway,
		},
		{
			name: "providers failed",
			err:  errors.New("all history providers failed for VOO (US-ETF): timeout"),
			want: http.StatusBadGateway,
		},
		{
			name: "history unconfigured",
			err:  errors.New("History provider is not configured"),
			want: http.StatusServiceUnavailable,
		},
		{name: "deadline", err: context.DeadlineExceeded, want: http.StatusServiceUnavailable},
		{
			name: "save failure",
			err:  errors.New("save state failed after item update: disk full"),
			want: http.StatusInternalServerError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := statusForError(tc.err); got != tc.want {
				t.Fatalf("statusForError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}
