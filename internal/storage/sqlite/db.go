package sqlite

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const (
	maxOpenConnections = 4
	busyTimeoutMillis  = 5000
)

// Open opens an InvestGo SQLite database with the connection policy required by
// the desktop application. Callers own the returned database and must close it.
func Open(path string) (*sql.DB, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}

	databaseURL := &url.URL{Scheme: "file", Path: filepath.ToSlash(absolutePath)}
	query := databaseURL.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMillis))
	databaseURL.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", databaseURL.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(maxOpenConnections)

	closeWithError := func(openErr error) (*sql.DB, error) {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("%w; close sqlite database: %w", openErr, closeErr)
		}
		return nil, openErr
	}

	if err := db.Ping(); err != nil {
		return closeWithError(fmt.Errorf("ping sqlite database: %w", err))
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return closeWithError(fmt.Errorf("enable sqlite WAL: %w", err))
	}
	if journalMode != "wal" {
		return closeWithError(fmt.Errorf("enable sqlite WAL: unexpected journal mode %q", journalMode))
	}

	return db, nil
}
