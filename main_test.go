package main

import (
	"path/filepath"
	"testing"
)

func TestStoragePathsForConfigDirectory(t *testing.T) {
	t.Parallel()

	jsonPath, databasePath := storagePathsForConfigDirectory(filepath.Join("root", "config"))
	if want := filepath.Join("root", "config", "investgo", "state.json"); jsonPath != want {
		t.Fatalf("JSON path = %q; want %q", jsonPath, want)
	}
	if want := filepath.Join("root", "config", "investgo", "investgo.db"); databasePath != want {
		t.Fatalf("database path = %q; want %q", databasePath, want)
	}
}

func TestStoragePathsFallbackToProjectDataDirectory(t *testing.T) {
	t.Parallel()

	jsonPath, databasePath := storagePathsForConfigDirectory("")
	if want := filepath.Join("data", "state.json"); jsonPath != want {
		t.Fatalf("JSON fallback = %q; want %q", jsonPath, want)
	}
	if want := filepath.Join("data", "investgo.db"); databasePath != want {
		t.Fatalf("database fallback = %q; want %q", databasePath, want)
	}
}
