package store

import (
	"path/filepath"
	"testing"
	"time"

	"investgo/internal/core"
)

func TestJSONRepositoryRoundTripUsesTypedState(t *testing.T) {
	t.Parallel()

	repository := NewJSONRepository(filepath.Join(t.TempDir(), "state.json"))
	loaded, found, err := repository.Load()
	if err != nil {
		t.Fatalf("Load() empty repository error = %v", err)
	}
	if found {
		t.Fatalf("Load() empty repository found = true; want false")
	}
	if len(loaded.Items) != 0 || len(loaded.Alerts) != 0 {
		t.Fatalf("Load() empty repository returned data: %+v", loaded)
	}

	updatedAt := time.Date(2026, time.August, 31, 12, 30, 0, 0, time.UTC)
	state := PersistedState{
		Items: []core.WatchlistItem{
			{
				ID:        "item-aapl",
				Symbol:    "AAPL",
				Name:      "Apple",
				Market:    "US-STOCK",
				Currency:  "USD",
				Tags:      []string{"technology"},
				UpdatedAt: updatedAt,
			},
		},
		Settings:  core.AppSettings{ProxyMode: "system"},
		UpdatedAt: updatedAt,
	}
	if err := repository.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	state.Items[0].Name = "mutated after save"
	state.Items[0].Tags[0] = "mutated"

	loaded, found, err = repository.Load()
	if err != nil {
		t.Fatalf("Load() saved repository error = %v", err)
	}
	if !found {
		t.Fatal("Load() saved repository found = false; want true")
	}
	if got := loaded.Items[0].Name; got != "Apple" {
		t.Fatalf("loaded item name = %q; want Apple", got)
	}
	if got := loaded.Items[0].Tags[0]; got != "technology" {
		t.Fatalf("loaded item tag = %q; want technology", got)
	}
	if !loaded.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("loaded UpdatedAt = %s; want %s", loaded.UpdatedAt, updatedAt)
	}
}
