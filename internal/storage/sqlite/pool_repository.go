package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"investgo/internal/core/instrument"
	"investgo/internal/core/pool"
)

// PoolRepository persists pool definitions, baseline membership, and user
// overrides without merging those layers in storage.
type PoolRepository struct {
	db *sql.DB
}

func NewPoolRepository(db *sql.DB) *PoolRepository {
	return &PoolRepository{db: db}
}

func (r *PoolRepository) Get(ctx context.Context, id string) (pool.Pool, bool, error) {
	if err := r.ready(); err != nil {
		return pool.Pool{}, false, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return pool.Pool{}, false, nil
	}
	return scanPool(r.db.QueryRowContext(ctx, poolSelect+" WHERE id = ?", id))
}

func (r *PoolRepository) List(ctx context.Context) ([]pool.Pool, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, poolSelect+" ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}
	defer rows.Close()

	values := make([]pool.Pool, 0)
	for rows.Next() {
		value, _, err := scanPool(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pool: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pools: %w", err)
	}
	return values, nil
}

func (r *PoolRepository) Upsert(ctx context.Context, value pool.Pool) (pool.Pool, error) {
	if err := r.ready(); err != nil {
		return pool.Pool{}, err
	}
	normalized, err := pool.Normalize(value)
	if err != nil {
		return pool.Pool{}, err
	}
	now := time.Now().UTC()
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	if normalized.UpdatedAt.IsZero() {
		normalized.UpdatedAt = now
	}

	stored, _, err := scanPool(r.db.QueryRowContext(ctx, `
		INSERT INTO pools(
			id, name, market, asset_class, pool_type, data_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			market = excluded.market,
			asset_class = excluded.asset_class,
			pool_type = excluded.pool_type,
			data_version = excluded.data_version,
			updated_at = excluded.updated_at
		RETURNING id, name, market, asset_class, pool_type, data_version, created_at, updated_at
	`,
		normalized.ID,
		normalized.Name,
		normalized.Market,
		normalized.AssetClass,
		normalized.Type,
		normalized.DataVersion,
		formatInstrumentTime(normalized.CreatedAt),
		formatInstrumentTime(normalized.UpdatedAt),
	))
	if err != nil {
		return pool.Pool{}, fmt.Errorf("upsert pool %s: %w", normalized.ID, err)
	}
	return stored, nil
}

func (r *PoolRepository) ReplaceBuiltInMembers(
	ctx context.Context,
	poolID string,
	dataVersion string,
	instrumentIDs []string,
) error {
	if err := r.ready(); err != nil {
		return err
	}
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return &pool.ValidationError{Field: "poolId", Message: "is required"}
	}
	dataVersion = strings.TrimSpace(dataVersion)
	if dataVersion == "" {
		return &pool.ValidationError{Field: "dataVersion", Message: "is required"}
	}
	instrumentIDs = uniqueSortedStrings(instrumentIDs)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin built-in membership replacement: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		UPDATE pools SET data_version = ?, updated_at = ? WHERE id = ?
	`, dataVersion, formatInstrumentTime(time.Now().UTC()), poolID)
	if err != nil {
		return fmt.Errorf("update pool data version %s: %w", poolID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect pool update %s: %w", poolID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("replace built-in members: pool %s does not exist", poolID)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM builtin_pool_members WHERE pool_id = ?", poolID); err != nil {
		return fmt.Errorf("clear built-in members for %s: %w", poolID, err)
	}
	if err := pruneBaselineOverlays(ctx, tx, poolID, instrumentIDs); err != nil {
		return err
	}
	createdAt := formatInstrumentTime(time.Now().UTC())
	for _, instrumentID := range instrumentIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO builtin_pool_members(pool_id, instrument_id, data_version, created_at)
			VALUES (?, ?, ?, ?)
		`, poolID, instrumentID, dataVersion, createdAt); err != nil {
			return fmt.Errorf("add built-in member %s to %s: %w", instrumentID, poolID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit built-in membership replacement: %w", err)
	}
	return nil
}

func (r *PoolRepository) ListBuiltInMemberIDs(ctx context.Context, poolID string) ([]string, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT instrument_id
		FROM builtin_pool_members
		WHERE pool_id = ?
		ORDER BY instrument_id
	`, strings.TrimSpace(poolID))
	if err != nil {
		return nil, fmt.Errorf("list built-in members for %s: %w", poolID, err)
	}
	defer rows.Close()

	values := make([]string, 0)
	for rows.Next() {
		var instrumentID string
		if err := rows.Scan(&instrumentID); err != nil {
			return nil, fmt.Errorf("scan built-in member for %s: %w", poolID, err)
		}
		values = append(values, instrumentID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate built-in members for %s: %w", poolID, err)
	}
	return values, nil
}

func (r *PoolRepository) GetOverride(
	ctx context.Context,
	poolID string,
	instrumentID string,
) (pool.Override, bool, error) {
	if err := r.ready(); err != nil {
		return pool.Override{}, false, err
	}
	return scanPoolOverride(r.db.QueryRowContext(ctx, `
		SELECT pool_id, instrument_id, action, created_at, updated_at
		FROM pool_member_overrides
		WHERE pool_id = ? AND instrument_id = ?
	`, strings.TrimSpace(poolID), strings.TrimSpace(instrumentID)))
}

func (r *PoolRepository) ListOverrides(ctx context.Context, poolID string) ([]pool.Override, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT pool_id, instrument_id, action, created_at, updated_at
		FROM pool_member_overrides
		WHERE pool_id = ?
		ORDER BY instrument_id
	`, strings.TrimSpace(poolID))
	if err != nil {
		return nil, fmt.Errorf("list pool overrides for %s: %w", poolID, err)
	}
	defer rows.Close()

	values := make([]pool.Override, 0)
	for rows.Next() {
		value, _, err := scanPoolOverride(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pool override: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pool overrides: %w", err)
	}
	return values, nil
}

func (r *PoolRepository) UpsertOverride(ctx context.Context, value pool.Override) (pool.Override, error) {
	if err := r.ready(); err != nil {
		return pool.Override{}, err
	}
	normalized, err := pool.NormalizeOverride(value)
	if err != nil {
		return pool.Override{}, err
	}
	now := time.Now().UTC()
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	if normalized.UpdatedAt.IsZero() {
		normalized.UpdatedAt = now
	}

	stored, _, err := scanPoolOverride(r.db.QueryRowContext(ctx, `
		INSERT INTO pool_member_overrides(pool_id, instrument_id, action, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(pool_id, instrument_id) DO UPDATE SET
			action = excluded.action,
			updated_at = excluded.updated_at
		RETURNING pool_id, instrument_id, action, created_at, updated_at
	`,
		normalized.PoolID,
		normalized.InstrumentID,
		normalized.Action,
		formatInstrumentTime(normalized.CreatedAt),
		formatInstrumentTime(normalized.UpdatedAt),
	))
	if err != nil {
		return pool.Override{}, fmt.Errorf(
			"upsert pool override %s/%s: %w",
			normalized.PoolID,
			normalized.InstrumentID,
			err,
		)
	}
	return stored, nil
}

func (r *PoolRepository) DeleteOverride(ctx context.Context, poolID, instrumentID string) error {
	if err := r.ready(); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(
		ctx,
		"DELETE FROM pool_member_overrides WHERE pool_id = ? AND instrument_id = ?",
		strings.TrimSpace(poolID),
		strings.TrimSpace(instrumentID),
	); err != nil {
		return fmt.Errorf("delete pool override %s/%s: %w", poolID, instrumentID, err)
	}
	return nil
}

func (r *PoolRepository) GetEdit(
	ctx context.Context,
	poolID string,
	instrumentID string,
) (pool.Edit, bool, error) {
	if err := r.ready(); err != nil {
		return pool.Edit{}, false, err
	}
	return scanPoolEdit(r.db.QueryRowContext(ctx, `
		SELECT pool_id, instrument_id, symbol, name, updated_at
		FROM pool_member_edits
		WHERE pool_id = ? AND instrument_id = ?
	`, strings.TrimSpace(poolID), strings.TrimSpace(instrumentID)))
}

func (r *PoolRepository) UpsertEdit(ctx context.Context, value pool.Edit) (pool.Edit, error) {
	if err := r.ready(); err != nil {
		return pool.Edit{}, err
	}
	normalized, err := pool.NormalizeEdit(value)
	if err != nil {
		return pool.Edit{}, err
	}
	now := time.Now().UTC()
	if normalized.UpdatedAt.IsZero() {
		normalized.UpdatedAt = now
	}

	stored, _, err := scanPoolEdit(r.db.QueryRowContext(ctx, `
		INSERT INTO pool_member_edits(pool_id, instrument_id, symbol, name, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(pool_id, instrument_id) DO UPDATE SET
			symbol = excluded.symbol,
			name = excluded.name,
			updated_at = excluded.updated_at
		RETURNING pool_id, instrument_id, symbol, name, updated_at
	`,
		normalized.PoolID,
		normalized.InstrumentID,
		normalized.Symbol,
		normalized.Name,
		formatInstrumentTime(normalized.UpdatedAt),
	))
	if err != nil {
		return pool.Edit{}, fmt.Errorf(
			"upsert pool edit %s/%s: %w",
			normalized.PoolID,
			normalized.InstrumentID,
			err,
		)
	}
	return stored, nil
}

func (r *PoolRepository) DeleteEdit(ctx context.Context, poolID, instrumentID string) error {
	if err := r.ready(); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(
		ctx,
		"DELETE FROM pool_member_edits WHERE pool_id = ? AND instrument_id = ?",
		strings.TrimSpace(poolID),
		strings.TrimSpace(instrumentID),
	); err != nil {
		return fmt.Errorf("delete pool edit %s/%s: %w", poolID, instrumentID, err)
	}
	return nil
}

func (r *PoolRepository) ready() error {
	if r == nil || r.db == nil {
		return errors.New("sqlite pool repository is not configured")
	}
	return nil
}

const poolSelect = `
	SELECT id, name, market, asset_class, pool_type, data_version, created_at, updated_at
	FROM pools
`

func scanPool(row rowScanner) (pool.Pool, bool, error) {
	var value pool.Pool
	var assetClass string
	var poolType string
	var createdAtRaw string
	var updatedAtRaw string
	if err := row.Scan(
		&value.ID,
		&value.Name,
		&value.Market,
		&assetClass,
		&poolType,
		&value.DataVersion,
		&createdAtRaw,
		&updatedAtRaw,
	); errors.Is(err, sql.ErrNoRows) {
		return pool.Pool{}, false, nil
	} else if err != nil {
		return pool.Pool{}, false, err
	}
	value.AssetClass = instrument.AssetClass(assetClass)
	value.Type = pool.Type(poolType)
	var err error
	value.CreatedAt, err = parseInstrumentTime(createdAtRaw)
	if err != nil {
		return pool.Pool{}, false, fmt.Errorf("parse pool created time: %w", err)
	}
	value.UpdatedAt, err = parseInstrumentTime(updatedAtRaw)
	if err != nil {
		return pool.Pool{}, false, fmt.Errorf("parse pool updated time: %w", err)
	}
	return value, true, nil
}

func scanPoolOverride(row rowScanner) (pool.Override, bool, error) {
	var value pool.Override
	var action string
	var createdAtRaw string
	var updatedAtRaw string
	if err := row.Scan(
		&value.PoolID,
		&value.InstrumentID,
		&action,
		&createdAtRaw,
		&updatedAtRaw,
	); errors.Is(err, sql.ErrNoRows) {
		return pool.Override{}, false, nil
	} else if err != nil {
		return pool.Override{}, false, err
	}
	value.Action = pool.OverrideAction(action)
	var err error
	value.CreatedAt, err = parseInstrumentTime(createdAtRaw)
	if err != nil {
		return pool.Override{}, false, fmt.Errorf("parse pool override created time: %w", err)
	}
	value.UpdatedAt, err = parseInstrumentTime(updatedAtRaw)
	if err != nil {
		return pool.Override{}, false, fmt.Errorf("parse pool override updated time: %w", err)
	}
	return value, true, nil
}

func scanPoolEdit(row rowScanner) (pool.Edit, bool, error) {
	var value pool.Edit
	var updatedAtRaw string
	if err := row.Scan(
		&value.PoolID,
		&value.InstrumentID,
		&value.Symbol,
		&value.Name,
		&updatedAtRaw,
	); errors.Is(err, sql.ErrNoRows) {
		return pool.Edit{}, false, nil
	} else if err != nil {
		return pool.Edit{}, false, err
	}
	var err error
	value.UpdatedAt, err = parseInstrumentTime(updatedAtRaw)
	if err != nil {
		return pool.Edit{}, false, fmt.Errorf("parse pool edit updated time: %w", err)
	}
	return value, true, nil
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// pruneBaselineOverlays drops overlays that no longer make sense after a
// baseline rewrite: edits for removed members, add overrides that have become
// built-in, and exclude overrides for instruments that left the universe.
func pruneBaselineOverlays(ctx context.Context, tx *sql.Tx, poolID string, instrumentIDs []string) error {
	if len(instrumentIDs) == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM pool_member_edits WHERE pool_id = ?", poolID); err != nil {
			return fmt.Errorf("prune stale member edits for %s: %w", poolID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM pool_member_overrides WHERE pool_id = ? AND action = 'exclude'
		`, poolID); err != nil {
			return fmt.Errorf("prune stale exclusions for %s: %w", poolID, err)
		}
		return nil
	}

	placeholders, args := sqlInClause(poolID, instrumentIDs)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM pool_member_edits WHERE pool_id = ? AND instrument_id NOT IN (%s)",
		placeholders,
	), args...); err != nil {
		return fmt.Errorf("prune stale member edits for %s: %w", poolID, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM pool_member_overrides WHERE pool_id = ? AND action = 'add' AND instrument_id IN (%s)",
		placeholders,
	), args...); err != nil {
		return fmt.Errorf("promote leftover additions for %s: %w", poolID, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM pool_member_overrides WHERE pool_id = ? AND action = 'exclude' AND instrument_id NOT IN (%s)",
		placeholders,
	), args...); err != nil {
		return fmt.Errorf("prune stale exclusions for %s: %w", poolID, err)
	}
	return nil
}

func sqlInClause(poolID string, instrumentIDs []string) (string, []any) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(instrumentIDs)), ",")
	args := make([]any, 0, len(instrumentIDs)+1)
	args = append(args, poolID)
	for _, instrumentID := range instrumentIDs {
		args = append(args, instrumentID)
	}
	return placeholders, args
}

var _ pool.Repository = (*PoolRepository)(nil)
