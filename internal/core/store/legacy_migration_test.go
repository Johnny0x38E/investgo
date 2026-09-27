package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"investgo/internal/core"
	sqlitestorage "investgo/internal/storage/sqlite"
)

func TestEnsureSQLiteStateMigratesJSONAndPreservesBackup(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "state.json")
	databasePath := filepath.Join(directory, "investgo.db")
	updatedAt := time.Date(2026, time.August, 31, 16, 0, 0, 0, time.UTC)
	want := PersistedState{
		Items: []core.WatchlistItem{
			{
				ID:        "item-0700",
				Symbol:    "00700.HK",
				Name:      "腾讯控股",
				Market:    "HK-MAIN",
				Currency:  "HKD",
				Quantity:  12,
				CostPrice: 501.25,
				Tags:      []string{"互联网", "长期"},
				DCAEntries: []core.DCAEntry{
					{
						ID:     "dca-0700",
						Date:   updatedAt.AddDate(0, -1, 0),
						Amount: 2500,
						Shares: 5,
						Fee:    12.5,
					},
				},
				UpdatedAt: updatedAt.Add(-time.Minute),
			},
		},
		Alerts: []core.AlertRule{
			{
				ID:        "alert-0700",
				ItemID:    "item-0700",
				Name:      "腾讯价格提醒",
				Condition: core.AlertBelow,
				Threshold: 480,
				Enabled:   true,
				UpdatedAt: updatedAt,
			},
		},
		Settings:  core.AppSettings{ProxyMode: "system", DashboardCurrency: "CNY"},
		UpdatedAt: updatedAt,
	}
	if err := NewJSONRepository(jsonPath).Save(want); err != nil {
		t.Fatalf("write legacy JSON: %v", err)
	}

	result, err := EnsureSQLiteState(context.Background(), jsonPath, databasePath)
	if err != nil {
		t.Fatalf("EnsureSQLiteState() error = %v", err)
	}
	if !result.Migrated {
		t.Fatal("EnsureSQLiteState() Migrated = false; want true")
	}
	if result.DatabasePath != databasePath {
		t.Fatalf("DatabasePath = %q; want %q", result.DatabasePath, databasePath)
	}
	if result.BackupPath == "" {
		t.Fatal("BackupPath is empty after JSON migration")
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("source JSON was not preserved: %v", err)
	}

	backupState, found, err := NewJSONRepository(result.BackupPath).Load()
	if err != nil {
		t.Fatalf("load JSON backup: %v", err)
	}
	if !found || !reflect.DeepEqual(backupState, want) {
		t.Fatalf("backup mismatch\ngot:  %#v\nwant: %#v", backupState, want)
	}

	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	got, found, err := NewSQLiteRepository(db, databasePath).Load()
	if err != nil {
		t.Fatalf("load migrated state: %v", err)
	}
	wantSQLite := want
	wantSQLite.Items = append([]core.WatchlistItem(nil), want.Items...)
	wantSQLite.Items[0].Symbol = "00700"
	if !found || !reflect.DeepEqual(got, wantSQLite) {
		t.Fatalf("migrated state mismatch\ngot:  %#v\nwant: %#v", got, wantSQLite)
	}
}

func TestEnsureSQLiteStateCreatesSchemaWithoutLegacyJSON(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "state.json")
	databasePath := filepath.Join(directory, "investgo.db")

	result, err := EnsureSQLiteState(context.Background(), jsonPath, databasePath)
	if err != nil {
		t.Fatalf("EnsureSQLiteState() error = %v", err)
	}
	if result.Migrated {
		t.Fatal("Migrated = true; want false without legacy JSON")
	}
	if result.BackupPath != "" {
		t.Fatalf("BackupPath = %q; want empty", result.BackupPath)
	}

	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("open created database: %v", err)
	}
	defer db.Close()
	version, err := sqlitestorage.SchemaVersion(context.Background(), db)
	if err != nil {
		t.Fatalf("SchemaVersion() error = %v", err)
	}
	if version != 3 {
		t.Fatalf("SchemaVersion() = %d; want 3", version)
	}
	_, found, err := NewSQLiteRepository(db, databasePath).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if found {
		t.Fatal("Load() found = true; want false until Store seeds initial state")
	}
}

func TestEnsureSQLiteStateReusesExistingDatabase(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "state.json")
	databasePath := filepath.Join(directory, "investgo.db")
	updatedAt := time.Date(2026, time.August, 31, 17, 0, 0, 0, time.UTC)
	databaseState := PersistedState{
		Items: []core.WatchlistItem{
			{ID: "item-db", Symbol: "AAPL", Name: "database", Market: "US-STOCK", Currency: "USD", UpdatedAt: updatedAt},
		},
		Alerts:    []core.AlertRule{},
		Settings:  core.AppSettings{ProxyMode: "none"},
		UpdatedAt: updatedAt,
	}
	jsonState := PersistedState{
		Items: []core.WatchlistItem{
			{ID: "item-json", Symbol: "MSFT", Name: "legacy JSON", Market: "US-STOCK", Currency: "USD", UpdatedAt: updatedAt},
		},
		Alerts:    []core.AlertRule{},
		Settings:  core.AppSettings{ProxyMode: "system"},
		UpdatedAt: updatedAt,
	}

	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := sqlitestorage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if err := NewSQLiteRepository(db, databasePath).Save(databaseState); err != nil {
		t.Fatalf("save database state: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if err := NewJSONRepository(jsonPath).Save(jsonState); err != nil {
		t.Fatalf("save legacy JSON: %v", err)
	}

	result, err := EnsureSQLiteState(context.Background(), jsonPath, databasePath)
	if err != nil {
		t.Fatalf("EnsureSQLiteState() error = %v", err)
	}
	if result.Migrated || result.BackupPath != "" {
		t.Fatalf("existing database result = %+v; want no migration", result)
	}

	db, err = sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer db.Close()
	got, found, err := NewSQLiteRepository(db, databasePath).Load()
	if err != nil {
		t.Fatalf("load database: %v", err)
	}
	if !found || !reflect.DeepEqual(got, databaseState) {
		t.Fatalf("existing database was replaced\ngot:  %#v\nwant: %#v", got, databaseState)
	}
}

func TestEnsureSQLiteStateInvalidJSONDoesNotPublishDatabase(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "state.json")
	databasePath := filepath.Join(directory, "investgo.db")
	invalidPayload := []byte(`{"items":`)
	if err := os.WriteFile(jsonPath, invalidPayload, 0o600); err != nil {
		t.Fatalf("write invalid JSON: %v", err)
	}

	if _, err := EnsureSQLiteState(context.Background(), jsonPath, databasePath); err == nil {
		t.Fatal("EnsureSQLiteState() error = nil; want invalid JSON error")
	}
	assertPathMissing(t, databasePath)
	assertPathMissing(t, databasePath+".tmp")
	assertPathMissing(t, databasePath+".tmp-wal")
	assertPathMissing(t, databasePath+".tmp-shm")
	gotPayload, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read source JSON: %v", err)
	}
	if !reflect.DeepEqual(gotPayload, invalidPayload) {
		t.Fatalf("source JSON changed: got %q; want %q", gotPayload, invalidPayload)
	}
}

func TestEnsureSQLiteStateConstraintFailureCleansOnlyTemporaryDatabase(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "state.json")
	databasePath := filepath.Join(directory, "investgo.db")
	updatedAt := time.Date(2026, time.August, 31, 18, 0, 0, 0, time.UTC)
	invalidState := PersistedState{
		Items: []core.WatchlistItem{},
		Alerts: []core.AlertRule{
			{
				ID:        "orphan-alert",
				ItemID:    "missing-item",
				Name:      "orphan",
				Condition: core.AlertAbove,
				Threshold: 1,
				UpdatedAt: updatedAt,
			},
		},
		Settings:  core.AppSettings{ProxyMode: "system"},
		UpdatedAt: updatedAt,
	}
	if err := NewJSONRepository(jsonPath).Save(invalidState); err != nil {
		t.Fatalf("save invalid legacy state: %v", err)
	}
	before, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read legacy JSON before migration: %v", err)
	}

	if _, err := EnsureSQLiteState(context.Background(), jsonPath, databasePath); err == nil {
		t.Fatal("EnsureSQLiteState() error = nil; want foreign-key failure")
	}
	assertPathMissing(t, databasePath)
	assertPathMissing(t, databasePath+".tmp")
	assertPathMissing(t, databasePath+".tmp-wal")
	assertPathMissing(t, databasePath+".tmp-shm")
	after, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read legacy JSON after migration: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("legacy JSON changed after failed migration")
	}
	assertPathMissing(t, jsonPath+".backup")
}

func TestEnsureSQLiteStateMigrationIsIdempotent(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "state.json")
	databasePath := filepath.Join(directory, "investgo.db")
	updatedAt := time.Date(2026, time.August, 31, 19, 0, 0, 0, time.UTC)
	initial := PersistedState{
		Items: []core.WatchlistItem{
			{ID: "item-first", Symbol: "QQQ", Name: "first", Market: "US-ETF", Currency: "USD", UpdatedAt: updatedAt},
		},
		Alerts:    []core.AlertRule{},
		Settings:  core.AppSettings{ProxyMode: "system"},
		UpdatedAt: updatedAt,
	}
	if err := NewJSONRepository(jsonPath).Save(initial); err != nil {
		t.Fatalf("save initial JSON: %v", err)
	}
	first, err := EnsureSQLiteState(context.Background(), jsonPath, databasePath)
	if err != nil {
		t.Fatalf("first EnsureSQLiteState() error = %v", err)
	}
	if !first.Migrated {
		t.Fatal("first migration did not report Migrated")
	}

	replacement := clonePersistedState(initial)
	replacement.Items[0].Name = "changed JSON after migration"
	if err := NewJSONRepository(jsonPath).Save(replacement); err != nil {
		t.Fatalf("replace legacy JSON: %v", err)
	}
	second, err := EnsureSQLiteState(context.Background(), jsonPath, databasePath)
	if err != nil {
		t.Fatalf("second EnsureSQLiteState() error = %v", err)
	}
	if second.Migrated || second.BackupPath != "" {
		t.Fatalf("second result = %+v; want existing database reuse", second)
	}

	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer db.Close()
	got, found, err := NewSQLiteRepository(db, databasePath).Load()
	if err != nil {
		t.Fatalf("load migrated database: %v", err)
	}
	if !found || !reflect.DeepEqual(got, initial) {
		t.Fatalf("idempotent state mismatch\ngot:  %#v\nwant: %#v", got, initial)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %s exists or stat failed with %v; want not exist", path, err)
	}
}
