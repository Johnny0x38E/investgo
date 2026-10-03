package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const createMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
)`

type migration struct {
	version int
	name    string
	sql     string
}

// ApplyMigrations applies all embedded migrations that are newer than the
// database schema version. Re-running it against an up-to-date database is safe.
func ApplyMigrations(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("apply sqlite migrations: database is nil")
	}
	if _, err := db.ExecContext(ctx, createMigrationsTableSQL); err != nil {
		return fmt.Errorf("create sqlite migrations table: %w", err)
	}

	currentVersion, err := SchemaVersion(ctx, db)
	if err != nil {
		return err
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, candidate := range migrations {
		if candidate.version <= currentVersion {
			continue
		}
		if err := applyMigration(ctx, db, candidate); err != nil {
			return err
		}
		currentVersion = candidate.version
	}
	return nil
}

// SchemaVersion returns the highest successfully applied migration version.
// A database without the migration metadata table has version zero.
func SchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("read sqlite schema version: database is nil")
	}

	var exists int
	if err := db.QueryRowContext(ctx, `
        SELECT COUNT(*)
        FROM sqlite_master
        WHERE type = 'table' AND name = 'schema_migrations'
    `).Scan(&exists); err != nil {
		return 0, fmt.Errorf("find sqlite migrations table: %w", err)
	}
	if exists == 0 {
		return 0, nil
	}

	var version int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").
		Scan(&version); err != nil {
		return 0, fmt.Errorf("read sqlite schema version: %w", err)
	}
	return version, nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded sqlite migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	seenVersions := make(map[int]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version, err := migrationVersion(entry.Name())
		if err != nil {
			return nil, err
		}
		if previous, exists := seenVersions[version]; exists {
			return nil, fmt.Errorf(
				"duplicate sqlite migration version %d in %s and %s",
				version,
				previous,
				entry.Name(),
			)
		}

		path := "migrations/" + entry.Name()
		payload, err := migrationFiles.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read sqlite migration %s: %w", entry.Name(), err)
		}
		if strings.TrimSpace(string(payload)) == "" {
			return nil, fmt.Errorf("sqlite migration %s is empty", entry.Name())
		}

		seenVersions[version] = entry.Name()
		migrations = append(migrations, migration{version: version, name: entry.Name(), sql: string(payload)})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	return migrations, nil
}

func migrationVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok || prefix == "" {
		return 0, fmt.Errorf("sqlite migration filename %q must start with a numeric version and underscore", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("sqlite migration filename %q has invalid version", name)
	}
	return version, nil
}

func applyMigration(ctx context.Context, db *sql.DB, candidate migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite migration %s: %w", candidate.name, err)
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, candidate.sql); err != nil {
		return fmt.Errorf("apply sqlite migration %s: %w", candidate.name, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		"INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)",
		candidate.version,
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("record sqlite migration %s: %w", candidate.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite migration %s: %w", candidate.name, err)
	}
	return nil
}
