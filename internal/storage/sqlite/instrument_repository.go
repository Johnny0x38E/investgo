package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"investgo/internal/core/instrument"
)

// InstrumentRepository persists the canonical instrument catalog and provider
// mappings in SQLite. The database must have current migrations applied.
type InstrumentRepository struct {
	db *sql.DB
}

func NewInstrumentRepository(db *sql.DB) *InstrumentRepository {
	return &InstrumentRepository{db: db}
}

func (r *InstrumentRepository) Get(ctx context.Context, id string) (instrument.Instrument, bool, error) {
	if err := r.ready(); err != nil {
		return instrument.Instrument{}, false, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return instrument.Instrument{}, false, nil
	}
	return scanInstrument(r.db.QueryRowContext(ctx, instrumentSelect+" WHERE id = ?", id))
}

func (r *InstrumentRepository) Find(
	ctx context.Context,
	identity instrument.Identity,
) (instrument.Instrument, bool, error) {
	if err := r.ready(); err != nil {
		return instrument.Instrument{}, false, err
	}
	normalized, err := instrument.NormalizeIdentity(identity)
	if err != nil {
		return instrument.Instrument{}, false, err
	}
	return scanInstrument(r.db.QueryRowContext(
		ctx,
		instrumentSelect+" WHERE asset_class = ? AND market = ? AND exchange = ? AND symbol = ?",
		normalized.AssetClass,
		normalized.Market,
		normalized.Exchange,
		normalized.Symbol,
	))
}

func (r *InstrumentRepository) Upsert(ctx context.Context, value instrument.Instrument) (instrument.Instrument, error) {
	if err := r.ready(); err != nil {
		return instrument.Instrument{}, err
	}
	normalized, err := instrument.Normalize(value)
	if err != nil {
		return instrument.Instrument{}, err
	}
	if normalized.ID == "" {
		normalized.ID, err = instrument.CanonicalID(normalized.Identity())
		if err != nil {
			return instrument.Instrument{}, err
		}
	}
	now := time.Now().UTC()
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	if normalized.UpdatedAt.IsZero() {
		normalized.UpdatedAt = now
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return instrument.Instrument{}, fmt.Errorf("begin instrument upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck

	stored, _, err := scanInstrument(tx.QueryRowContext(ctx, `
		INSERT INTO instruments(
			id, asset_class, symbol, name, display_name, market, exchange, base_asset,
			quote_currency, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(asset_class, market, exchange, symbol) DO UPDATE SET
			name = excluded.name,
			base_asset = excluded.base_asset,
			quote_currency = excluded.quote_currency,
			status = excluded.status,
			updated_at = excluded.updated_at
		RETURNING `+instrumentReturning+`
	`,
		normalized.ID,
		normalized.AssetClass,
		normalized.Symbol,
		normalized.Name,
		normalized.DisplayName,
		normalized.Market,
		normalized.Exchange,
		normalized.BaseAsset,
		normalized.QuoteCurrency,
		normalized.Status,
		formatInstrumentTime(normalized.CreatedAt),
		formatInstrumentTime(normalized.UpdatedAt),
	))
	if err != nil {
		return instrument.Instrument{}, fmt.Errorf("upsert instrument %s: %w", normalized.Symbol, err)
	}
	if err := tx.Commit(); err != nil {
		return instrument.Instrument{}, fmt.Errorf("commit instrument upsert: %w", err)
	}
	return stored, nil
}

func (r *InstrumentRepository) SetDisplayName(
	ctx context.Context,
	id, displayName string,
) (instrument.Instrument, error) {
	if err := r.ready(); err != nil {
		return instrument.Instrument{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return instrument.Instrument{}, &instrument.ValidationError{Field: "id", Message: "is required"}
	}
	stored, found, err := scanInstrument(r.db.QueryRowContext(ctx, `
		UPDATE instruments
		SET display_name = ?, updated_at = ?
		WHERE id = ?
		RETURNING `+instrumentReturning+`
	`, strings.TrimSpace(displayName), formatInstrumentTime(time.Now().UTC()), id))
	if err != nil {
		return instrument.Instrument{}, fmt.Errorf("set display name %s: %w", id, err)
	}
	if !found {
		return instrument.Instrument{}, fmt.Errorf("set display name: instrument %s does not exist", id)
	}
	return stored, nil
}

func (r *InstrumentRepository) GetProviderSymbol(
	ctx context.Context,
	instrumentID string,
	providerID string,
) (instrument.ProviderSymbol, bool, error) {
	if err := r.ready(); err != nil {
		return instrument.ProviderSymbol{}, false, err
	}
	instrumentID = strings.TrimSpace(instrumentID)
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	if instrumentID == "" || providerID == "" {
		return instrument.ProviderSymbol{}, false, nil
	}
	return scanProviderSymbol(r.db.QueryRowContext(ctx, `
		SELECT instrument_id, provider_id, provider_symbol, metadata_json, updated_at
		FROM provider_symbols
		WHERE instrument_id = ? AND provider_id = ?
	`, instrumentID, providerID))
}

func (r *InstrumentRepository) UpsertProviderSymbol(
	ctx context.Context,
	value instrument.ProviderSymbol,
) (instrument.ProviderSymbol, error) {
	if err := r.ready(); err != nil {
		return instrument.ProviderSymbol{}, err
	}
	normalized, err := instrument.NormalizeProviderSymbol(value)
	if err != nil {
		return instrument.ProviderSymbol{}, err
	}
	if normalized.UpdatedAt.IsZero() {
		normalized.UpdatedAt = time.Now().UTC()
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return instrument.ProviderSymbol{}, fmt.Errorf("begin provider symbol upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck

	stored, _, err := scanProviderSymbol(tx.QueryRowContext(ctx, `
		INSERT INTO provider_symbols(
			instrument_id, provider_id, provider_symbol, metadata_json, updated_at
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(instrument_id, provider_id) DO UPDATE SET
			provider_symbol = excluded.provider_symbol,
			metadata_json = excluded.metadata_json,
			updated_at = excluded.updated_at
		RETURNING instrument_id, provider_id, provider_symbol, metadata_json, updated_at
	`,
		normalized.InstrumentID,
		normalized.ProviderID,
		normalized.Symbol,
		normalized.MetadataJSON,
		formatInstrumentTime(normalized.UpdatedAt),
	))
	if err != nil {
		return instrument.ProviderSymbol{}, fmt.Errorf(
			"upsert provider symbol %s/%s: %w",
			normalized.InstrumentID,
			normalized.ProviderID,
			err,
		)
	}
	if err := tx.Commit(); err != nil {
		return instrument.ProviderSymbol{}, fmt.Errorf("commit provider symbol upsert: %w", err)
	}
	return stored, nil
}

func (r *InstrumentRepository) DeleteProviderSymbol(ctx context.Context, instrumentID, providerID string) error {
	if err := r.ready(); err != nil {
		return err
	}
	instrumentID = strings.TrimSpace(instrumentID)
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	if instrumentID == "" || providerID == "" {
		return nil
	}
	if _, err := r.db.ExecContext(
		ctx,
		"DELETE FROM provider_symbols WHERE instrument_id = ? AND provider_id = ?",
		instrumentID,
		providerID,
	); err != nil {
		return fmt.Errorf("delete provider symbol %s/%s: %w", instrumentID, providerID, err)
	}
	return nil
}

func (r *InstrumentRepository) ready() error {
	if r == nil || r.db == nil {
		return errors.New("sqlite instrument repository is not configured")
	}
	return nil
}

const instrumentReturning = `
	id, asset_class, symbol, name, display_name, market, exchange, base_asset,
	quote_currency, status, created_at, updated_at
`

const instrumentSelect = `
	SELECT ` + instrumentReturning + `
	FROM instruments
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInstrument(row rowScanner) (instrument.Instrument, bool, error) {
	var value instrument.Instrument
	var assetClass string
	var status string
	var createdAtRaw string
	var updatedAtRaw string
	if err := row.Scan(
		&value.ID,
		&assetClass,
		&value.Symbol,
		&value.Name,
		&value.DisplayName,
		&value.Market,
		&value.Exchange,
		&value.BaseAsset,
		&value.QuoteCurrency,
		&status,
		&createdAtRaw,
		&updatedAtRaw,
	); errors.Is(err, sql.ErrNoRows) {
		return instrument.Instrument{}, false, nil
	} else if err != nil {
		return instrument.Instrument{}, false, err
	}
	value.AssetClass = instrument.AssetClass(assetClass)
	value.Status = instrument.InstrumentStatus(status)
	var err error
	value.CreatedAt, err = parseInstrumentTime(createdAtRaw)
	if err != nil {
		return instrument.Instrument{}, false, fmt.Errorf("parse instrument created time: %w", err)
	}
	value.UpdatedAt, err = parseInstrumentTime(updatedAtRaw)
	if err != nil {
		return instrument.Instrument{}, false, fmt.Errorf("parse instrument updated time: %w", err)
	}
	return value, true, nil
}

func scanProviderSymbol(row rowScanner) (instrument.ProviderSymbol, bool, error) {
	var value instrument.ProviderSymbol
	var updatedAtRaw string
	if err := row.Scan(
		&value.InstrumentID,
		&value.ProviderID,
		&value.Symbol,
		&value.MetadataJSON,
		&updatedAtRaw,
	); errors.Is(err, sql.ErrNoRows) {
		return instrument.ProviderSymbol{}, false, nil
	} else if err != nil {
		return instrument.ProviderSymbol{}, false, err
	}
	var err error
	value.UpdatedAt, err = parseInstrumentTime(updatedAtRaw)
	if err != nil {
		return instrument.ProviderSymbol{}, false, fmt.Errorf("parse provider symbol updated time: %w", err)
	}
	return value, true, nil
}

func formatInstrumentTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseInstrumentTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

var _ instrument.Repository = (*InstrumentRepository)(nil)
