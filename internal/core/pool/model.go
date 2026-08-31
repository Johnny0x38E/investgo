package pool

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"investgo/internal/core/instrument"
)

const (
	PoolIDCNA      = "pool-cn-a-ranking"
	PoolIDCNETF    = "pool-cn-etf-ranking"
	PoolIDHK       = "pool-hk-ranking"
	PoolIDUSSP500  = "pool-us-sp500"
	PoolIDUSNasdaq = "pool-us-nasdaq-100"
	PoolIDUSDow    = "pool-us-dow-30"
	PoolIDUSETF    = "pool-us-etf"
	PoolIDHKETF    = "pool-hk-etf"
)

type Type string

const (
	TypeBuiltIn Type = "builtin"
	TypeIndex   Type = "index"
	TypeCustom  Type = "custom"
)

type OverrideAction string

const (
	OverrideActionAdd     OverrideAction = "add"
	OverrideActionExclude OverrideAction = "exclude"
)

type MemberSource string

const (
	MemberSourceBuiltIn MemberSource = "builtin"
	MemberSourceUser    MemberSource = "user"
)

type MemberStatus string

const (
	MemberStatusActive   MemberStatus = "active"
	MemberStatusExcluded MemberStatus = "excluded"
)

type Pool struct {
	ID          string
	Name        string
	Market      string
	AssetClass  instrument.AssetClass
	Type        Type
	DataVersion string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Override struct {
	PoolID       string
	InstrumentID string
	Action       OverrideAction
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Edit records user edits to a built-in pool member. An empty Symbol or Name
// means "keep the baseline value". Edits never mutate shipped baseline rows;
// they are applied as an overlay when membership is read.
type Edit struct {
	PoolID       string
	InstrumentID string
	Symbol       string
	Name         string
	UpdatedAt    time.Time
}

type Member struct {
	PoolID     string
	Instrument instrument.Instrument
	Source     MemberSource
	Status     MemberStatus
	UpdatedAt  time.Time
}

var ErrInvalidPool = errors.New("invalid pool")

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid pool %s: %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return ErrInvalidPool
}

func Normalize(value Pool) (Pool, error) {
	value.ID = strings.TrimSpace(value.ID)
	if value.ID == "" {
		return Pool{}, invalid("id", "is required")
	}
	value.Name = strings.TrimSpace(value.Name)
	if value.Name == "" {
		return Pool{}, invalid("name", "is required")
	}
	value.Market = strings.ToUpper(strings.TrimSpace(value.Market))
	if value.Market == "" {
		return Pool{}, invalid("market", "is required")
	}
	value.AssetClass = instrument.AssetClass(strings.ToLower(strings.TrimSpace(string(value.AssetClass))))
	switch value.AssetClass {
	case instrument.AssetClassEquity, instrument.AssetClassETF, instrument.AssetClassCrypto:
	default:
		return Pool{}, invalid("assetClass", "must be equity, etf, or crypto")
	}
	value.Type = Type(strings.ToLower(strings.TrimSpace(string(value.Type))))
	switch value.Type {
	case TypeBuiltIn, TypeIndex, TypeCustom:
	default:
		return Pool{}, invalid("type", "must be builtin, index, or custom")
	}
	value.DataVersion = strings.TrimSpace(value.DataVersion)
	if !value.CreatedAt.IsZero() {
		value.CreatedAt = value.CreatedAt.UTC()
	}
	if !value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.UpdatedAt.UTC()
	}
	return value, nil
}

func NormalizeOverride(value Override) (Override, error) {
	value.PoolID = strings.TrimSpace(value.PoolID)
	if value.PoolID == "" {
		return Override{}, invalid("poolId", "is required")
	}
	value.InstrumentID = strings.TrimSpace(value.InstrumentID)
	if value.InstrumentID == "" {
		return Override{}, invalid("instrumentId", "is required")
	}
	value.Action = OverrideAction(strings.ToLower(strings.TrimSpace(string(value.Action))))
	if value.Action != OverrideActionAdd && value.Action != OverrideActionExclude {
		return Override{}, invalid("action", "must be add or exclude")
	}
	if !value.CreatedAt.IsZero() {
		value.CreatedAt = value.CreatedAt.UTC()
	}
	if !value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.UpdatedAt.UTC()
	}
	return value, nil
}

func NormalizeEdit(value Edit) (Edit, error) {
	value.PoolID = strings.TrimSpace(value.PoolID)
	if value.PoolID == "" {
		return Edit{}, invalid("poolId", "is required")
	}
	value.InstrumentID = strings.TrimSpace(value.InstrumentID)
	if value.InstrumentID == "" {
		return Edit{}, invalid("instrumentId", "is required")
	}
	value.Symbol = strings.TrimSpace(value.Symbol)
	value.Name = strings.TrimSpace(value.Name)
	if value.Symbol == "" && value.Name == "" {
		return Edit{}, invalid("edit", "symbol or name is required")
	}
	if !value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.UpdatedAt.UTC()
	}
	return value, nil
}

func invalid(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
