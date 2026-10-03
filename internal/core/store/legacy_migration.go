package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	sqlitestorage "investgo/internal/storage/sqlite"
)

// MigrationResult describes the durable storage selected during startup.
type MigrationResult struct {
	Migrated     bool
	DatabasePath string
	BackupPath   string
}

// EnsureSQLiteState creates or validates the SQLite database and atomically
// migrates a legacy JSON state when one exists.
func EnsureSQLiteState(ctx context.Context, jsonPath, databasePath string) (MigrationResult, error) {
	result := MigrationResult{DatabasePath: databasePath}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	exists, err := regularFileExists(databasePath)
	if err != nil {
		return result, fmt.Errorf("inspect sqlite state: %w", err)
	}
	if exists {
		if err := validateExistingSQLite(ctx, databasePath); err != nil {
			return result, err
		}
		return result, nil
	}

	legacyState, found, err := NewJSONRepository(jsonPath).Load()
	if err != nil {
		return result, fmt.Errorf("load legacy JSON state: %w", err)
	}

	temporaryPath := databasePath + ".tmp"
	cleanupSQLiteFiles(temporaryPath)
	published := false
	defer func() {
		if !published {
			cleanupSQLiteFiles(temporaryPath)
		}
	}()

	if err := buildTemporarySQLite(ctx, temporaryPath, legacyState, found); err != nil {
		return result, err
	}

	if found {
		backupPath, err := backupLegacyJSON(jsonPath)
		if err != nil {
			return result, err
		}
		result.BackupPath = backupPath
	}

	if err := os.Rename(temporaryPath, databasePath); err != nil {
		return result, fmt.Errorf("publish sqlite state: %w", err)
	}
	published = true
	cleanupSQLiteSidecars(temporaryPath)
	result.Migrated = found
	return result, nil
}

func validateExistingSQLite(ctx context.Context, databasePath string) error {
	db, err := sqlitestorage.Open(databasePath)
	if err != nil {
		return fmt.Errorf("open existing sqlite state: %w", err)
	}
	defer func() { _ = db.Close() }() //nolint:errcheck

	if err := sqlitestorage.ApplyMigrations(ctx, db); err != nil {
		return fmt.Errorf("migrate existing sqlite state: %w", err)
	}
	return validateForeignKeys(ctx, db)
}

func buildTemporarySQLite(
	ctx context.Context,
	temporaryPath string,
	legacyState PersistedState,
	hasLegacyState bool,
) error {
	db, err := sqlitestorage.Open(temporaryPath)
	if err != nil {
		return fmt.Errorf("open temporary sqlite state: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = db.Close() //nolint:errcheck
		}
	}()

	if err := sqlitestorage.ApplyMigrations(ctx, db); err != nil {
		return fmt.Errorf("migrate temporary sqlite state: %w", err)
	}
	if hasLegacyState {
		repository := NewSQLiteRepository(db, temporaryPath)
		if err := repository.Save(legacyState); err != nil {
			return fmt.Errorf("import legacy JSON state: %w", err)
		}
		if err := validateImportedCounts(ctx, db, legacyState); err != nil {
			return err
		}
	}
	if err := validateForeignKeys(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("checkpoint temporary sqlite state: %w", err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("close temporary sqlite state: %w", err)
	}
	closed = true
	return nil
}

func validateImportedCounts(ctx context.Context, db *sql.DB, state PersistedState) error {
	dcaCount := 0
	for _, item := range state.Items {
		dcaCount += len(item.DCAEntries)
	}
	expectations := []struct {
		table string
		count int
	}{
		{table: "watchlist_entries", count: len(state.Items)},
		{table: "dca_entries", count: dcaCount},
		{table: "alerts", count: len(state.Alerts)},
	}

	for _, expectation := range expectations {
		var count int
		query := "SELECT COUNT(*) FROM " + expectation.table
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return fmt.Errorf("validate imported %s count: %w", expectation.table, err)
		}
		if count != expectation.count {
			return fmt.Errorf(
				"validate imported %s count: got %d, want %d",
				expectation.table,
				count,
				expectation.count,
			)
		}
	}
	return nil
}

func validateForeignKeys(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("validate sqlite foreign keys: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	if rows.Next() {
		return errors.New("validate sqlite foreign keys: violation found")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("validate sqlite foreign keys: %w", err)
	}
	return nil
}

func backupLegacyJSON(jsonPath string) (string, error) {
	backupPath := jsonPath + ".backup"
	exists, err := regularFileExists(backupPath)
	if err != nil {
		return "", fmt.Errorf("inspect legacy JSON backup: %w", err)
	}
	if exists {
		backupPath = fmt.Sprintf("%s.%d", backupPath, time.Now().UTC().UnixNano())
	}

	source, err := os.Open(jsonPath)
	if err != nil {
		return "", fmt.Errorf("open legacy JSON for backup: %w", err)
	}
	defer source.Close() //nolint:errcheck

	sourceInfo, err := source.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect legacy JSON for backup: %w", err)
	}
	destination, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, sourceInfo.Mode().Perm())
	if err != nil {
		return "", fmt.Errorf("create legacy JSON backup: %w", err)
	}

	copyErr := error(nil)
	if _, err := io.Copy(destination, source); err != nil {
		copyErr = fmt.Errorf("copy legacy JSON backup: %w", err)
	} else if err := destination.Sync(); err != nil {
		copyErr = fmt.Errorf("sync legacy JSON backup: %w", err)
	}
	if err := destination.Close(); copyErr == nil && err != nil {
		copyErr = fmt.Errorf("close legacy JSON backup: %w", err)
	}
	if copyErr != nil {
		_ = os.Remove(backupPath) //nolint:errcheck
		return "", copyErr
	}
	return backupPath, nil
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s is not a regular file", filepath.Clean(path))
	}
	return true, nil
}

func cleanupSQLiteFiles(path string) {
	_ = os.Remove(path) //nolint:errcheck
	cleanupSQLiteSidecars(path)
}

func cleanupSQLiteSidecars(path string) {
	_ = os.Remove(path + "-wal") //nolint:errcheck
	_ = os.Remove(path + "-shm") //nolint:errcheck
}
