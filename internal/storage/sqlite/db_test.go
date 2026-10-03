package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenConfiguresSQLiteForDesktopUse(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "nested", "investgo.db")
	db, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d; want 1", foreignKeys)
	}

	var busyTimeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("read busy_timeout pragma: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d; want 5000", busyTimeout)
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("read journal_mode pragma: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q; want wal", journalMode)
	}
}

func TestApplyMigrationsIsIdempotent(t *testing.T) {
	t.Parallel()

	db, err := Open(filepath.Join(t.TempDir(), "investgo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	ctx := context.Background()
	if err := ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("first ApplyMigrations() error = %v", err)
	}
	if err := ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("second ApplyMigrations() error = %v", err)
	}

	version, err := SchemaVersion(ctx, db)
	if err != nil {
		t.Fatalf("SchemaVersion() error = %v", err)
	}
	if version != 4 {
		t.Fatalf("SchemaVersion() = %d; want 4", version)
	}
}
