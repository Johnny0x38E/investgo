package pool

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"investgo/internal/core/instrument"
)

var (
	ErrPoolNotFound       = errors.New("pool not found")
	ErrInstrumentNotFound = errors.New("instrument not found")
	ErrMemberNotFound     = errors.New("pool member not found")
	ErrInvalidOperation   = errors.New("invalid pool member operation")
)

// Service computes effective pool membership from the immutable baseline plus
// persistent user additions and exclusions.
type Service struct {
	instruments instrument.Repository
	pools       Repository
}

func NewService(instruments instrument.Repository, pools Repository) *Service {
	return &Service{instruments: instruments, pools: pools}
}

func (s *Service) ListPools(ctx context.Context) ([]Pool, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	return s.pools.List(ctx)
}

func (s *Service) ListMembers(ctx context.Context, poolID string) ([]Member, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	definition, err := s.requirePool(ctx, poolID)
	if err != nil {
		return nil, err
	}
	baselineIDs, err := s.pools.ListBuiltInMemberIDs(ctx, definition.ID)
	if err != nil {
		return nil, err
	}
	overrides, err := s.pools.ListOverrides(ctx, definition.ID)
	if err != nil {
		return nil, err
	}

	members := make(map[string]Member, len(baselineIDs)+len(overrides))
	for _, instrumentID := range baselineIDs {
		value, found, err := s.instruments.Get(ctx, instrumentID)
		if err != nil {
			return nil, fmt.Errorf("load built-in pool member %s: %w", instrumentID, err)
		}
		if !found {
			return nil, fmt.Errorf("%w: %s", ErrInstrumentNotFound, instrumentID)
		}
		value, updatedAt, err := s.applyMemberEdit(ctx, definition.ID, instrumentID, value, definition.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("apply member edit %s/%s: %w", definition.ID, instrumentID, err)
		}
		members[instrumentID] = Member{
			PoolID:     definition.ID,
			Instrument: value,
			Source:     MemberSourceBuiltIn,
			Status:     MemberStatusActive,
			UpdatedAt:  updatedAt,
		}
	}
	for _, override := range overrides {
		value, found, err := s.instruments.Get(ctx, override.InstrumentID)
		if err != nil {
			return nil, fmt.Errorf("load overridden pool member %s: %w", override.InstrumentID, err)
		}
		if !found {
			return nil, fmt.Errorf("%w: %s", ErrInstrumentNotFound, override.InstrumentID)
		}
		member := Member{
			PoolID:     definition.ID,
			Instrument: value,
			Source:     MemberSourceUser,
			Status:     MemberStatusActive,
			UpdatedAt:  override.UpdatedAt,
		}
		if override.Action == OverrideActionExclude {
			member.Source = MemberSourceBuiltIn
			member.Status = MemberStatusExcluded
		}
		members[override.InstrumentID] = member
	}

	result := make([]Member, 0, len(members))
	for _, member := range members {
		result = append(result, member)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Status != result[j].Status {
			return result[i].Status == MemberStatusActive
		}
		if result[i].Instrument.Symbol != result[j].Instrument.Symbol {
			return result[i].Instrument.Symbol < result[j].Instrument.Symbol
		}
		return result[i].Instrument.ID < result[j].Instrument.ID
	})
	return result, nil
}

func (s *Service) EffectiveMembers(ctx context.Context, poolID string) ([]instrument.Instrument, error) {
	members, err := s.ListMembers(ctx, poolID)
	if err != nil {
		return nil, err
	}
	result := make([]instrument.Instrument, 0, len(members))
	for _, member := range members {
		if member.Status == MemberStatusActive {
			result = append(result, member.Instrument)
		}
	}
	return result, nil
}

func (s *Service) ExcludedInstrumentKeys(ctx context.Context, poolID string) ([]instrument.Identity, error) {
	members, err := s.ListMembers(ctx, poolID)
	if err != nil {
		return nil, err
	}
	result := make([]instrument.Identity, 0)
	for _, member := range members {
		if member.Status == MemberStatusExcluded {
			result = append(result, member.Instrument.Identity())
		}
	}
	return result, nil
}

func (s *Service) AddMember(ctx context.Context, poolID string, candidate instrument.Instrument) (Member, error) {
	if err := s.ready(); err != nil {
		return Member{}, err
	}
	definition, err := s.requirePool(ctx, poolID)
	if err != nil {
		return Member{}, err
	}
	normalized, err := instrument.Normalize(candidate)
	if err != nil {
		return Member{}, err
	}
	if !poolAcceptsInstrument(definition, normalized) {
		return Member{}, fmt.Errorf(
			"%w: pool %s does not accept %s/%s",
			ErrInvalidOperation,
			definition.ID,
			normalized.AssetClass,
			normalized.Market,
		)
	}
	stored, err := s.instruments.Upsert(ctx, normalized)
	if err != nil {
		return Member{}, err
	}
	baseline, err := s.isBuiltInMember(ctx, definition.ID, stored.ID)
	if err != nil {
		return Member{}, err
	}
	if baseline {
		if err := s.pools.DeleteOverride(ctx, definition.ID, stored.ID); err != nil {
			return Member{}, err
		}
		return Member{
			PoolID:     definition.ID,
			Instrument: stored,
			Source:     MemberSourceBuiltIn,
			Status:     MemberStatusActive,
			UpdatedAt:  stored.UpdatedAt,
		}, nil
	}
	override, err := s.pools.UpsertOverride(ctx, Override{
		PoolID:       definition.ID,
		InstrumentID: stored.ID,
		Action:       OverrideActionAdd,
	})
	if err != nil {
		return Member{}, err
	}
	return Member{
		PoolID:     definition.ID,
		Instrument: stored,
		Source:     MemberSourceUser,
		Status:     MemberStatusActive,
		UpdatedAt:  override.UpdatedAt,
	}, nil
}

func (s *Service) ExcludeMember(ctx context.Context, poolID, instrumentID string) (Member, error) {
	if err := s.ready(); err != nil {
		return Member{}, err
	}
	definition, err := s.requirePool(ctx, poolID)
	if err != nil {
		return Member{}, err
	}
	value, err := s.requireInstrument(ctx, instrumentID)
	if err != nil {
		return Member{}, err
	}
	if !poolAcceptsInstrument(definition, value) {
		return Member{}, fmt.Errorf("%w: instrument does not belong to pool market", ErrInvalidOperation)
	}
	if existing, found, err := s.pools.GetOverride(ctx, definition.ID, value.ID); err != nil {
		return Member{}, err
	} else if found && existing.Action == OverrideActionAdd {
		return Member{}, fmt.Errorf("%w: user-added members must be deleted, not excluded", ErrInvalidOperation)
	}
	override, err := s.pools.UpsertOverride(ctx, Override{
		PoolID:       definition.ID,
		InstrumentID: value.ID,
		Action:       OverrideActionExclude,
	})
	if err != nil {
		return Member{}, err
	}
	return Member{
		PoolID:     definition.ID,
		Instrument: value,
		Source:     MemberSourceBuiltIn,
		Status:     MemberStatusExcluded,
		UpdatedAt:  override.UpdatedAt,
	}, nil
}

func (s *Service) RestoreMember(ctx context.Context, poolID, instrumentID string) (Member, error) {
	if err := s.ready(); err != nil {
		return Member{}, err
	}
	definition, err := s.requirePool(ctx, poolID)
	if err != nil {
		return Member{}, err
	}
	value, err := s.requireInstrument(ctx, instrumentID)
	if err != nil {
		return Member{}, err
	}
	override, found, err := s.pools.GetOverride(ctx, definition.ID, value.ID)
	if err != nil {
		return Member{}, err
	}
	if !found || override.Action != OverrideActionExclude {
		return Member{}, fmt.Errorf("%w: exclusion %s/%s", ErrMemberNotFound, definition.ID, value.ID)
	}
	if err := s.pools.DeleteOverride(ctx, definition.ID, value.ID); err != nil {
		return Member{}, err
	}
	return Member{
		PoolID:     definition.ID,
		Instrument: value,
		Source:     MemberSourceBuiltIn,
		Status:     MemberStatusActive,
		UpdatedAt:  override.UpdatedAt,
	}, nil
}

func (s *Service) DeleteUserMember(ctx context.Context, poolID, instrumentID string) error {
	if err := s.ready(); err != nil {
		return err
	}
	definition, err := s.requirePool(ctx, poolID)
	if err != nil {
		return err
	}
	value, err := s.requireInstrument(ctx, instrumentID)
	if err != nil {
		return err
	}
	override, found, err := s.pools.GetOverride(ctx, definition.ID, value.ID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: addition %s/%s", ErrMemberNotFound, definition.ID, value.ID)
	}
	if override.Action != OverrideActionAdd {
		return fmt.Errorf("%w: built-in exclusions must be restored", ErrInvalidOperation)
	}
	return s.pools.DeleteOverride(ctx, definition.ID, value.ID)
}

// UpdateMemberInput carries a user edit of a pool member. Only fields that are
// set are applied; the rest keep their current values. ResetName clears a
// display alias so every surface falls back to the official name.
type UpdateMemberInput struct {
	Symbol    string
	Name      string
	ResetName bool
}

// UpdateMember edits a pool member's display symbol and/or name and persists
// the change. Display names are instrument-level aliases so holdings and every
// pool that shares the instrument stay in sync. Built-in symbol edits remain a
// scoped overlay so shipped baseline rows stay immutable.
func (s *Service) UpdateMember(
	ctx context.Context,
	poolID string,
	instrumentID string,
	input UpdateMemberInput,
) (Member, error) {
	if err := s.ready(); err != nil {
		return Member{}, err
	}
	definition, err := s.requirePool(ctx, poolID)
	if err != nil {
		return Member{}, err
	}
	current, err := s.requireInstrument(ctx, instrumentID)
	if err != nil {
		return Member{}, err
	}
	members, err := s.ListMembers(ctx, definition.ID)
	if err != nil {
		return Member{}, err
	}
	var target *Member
	for index := range members {
		if members[index].Instrument.ID == current.ID {
			target = &members[index]
			break
		}
	}
	if target == nil {
		return Member{}, fmt.Errorf("%w: %s/%s", ErrMemberNotFound, definition.ID, current.ID)
	}
	if target.Status != MemberStatusActive {
		return Member{}, fmt.Errorf("%w: excluded members must be restored before editing", ErrInvalidOperation)
	}

	next := target.Instrument
	symbolInput := strings.TrimSpace(input.Symbol)
	nameInput := strings.TrimSpace(input.Name)
	next.Symbol = firstNonEmpty(symbolInput, next.Symbol)
	presentedName := next.Display()
	nextPresented := presentedName
	if input.ResetName {
		nextPresented = next.Name
	} else if nameInput != "" {
		nextPresented = nameInput
	}
	if next.Symbol == target.Instrument.Symbol && nextPresented == presentedName && !input.ResetName {
		return *target, nil
	}

	// The edited symbol must remain valid for the pool's market and must not
	// collide with another effective member of the same pool.
	normalized, err := instrument.Normalize(instrument.Instrument{
		AssetClass:    definition.AssetClass,
		Symbol:        next.Symbol,
		Name:          next.Name,
		Market:        next.Market,
		Exchange:      next.Exchange,
		QuoteCurrency: next.QuoteCurrency,
	})
	if err != nil {
		return Member{}, err
	}
	if !poolAcceptsInstrument(definition, normalized) {
		return Member{}, fmt.Errorf("%w: instrument does not belong to pool market", ErrInvalidOperation)
	}
	for _, member := range members {
		if member.Status != MemberStatusActive || member.Instrument.ID == target.Instrument.ID {
			continue
		}
		if member.Instrument.Identity() == normalized.Identity() {
			return Member{}, fmt.Errorf("%w: pool already contains this instrument", ErrInvalidOperation)
		}
	}

	if target.Source == MemberSourceUser && next.Symbol != target.Instrument.Symbol {
		stored, err := s.instruments.Upsert(ctx, normalized)
		if err != nil {
			return Member{}, err
		}
		if stored.ID != target.Instrument.ID {
			// The symbol changed identity: move the add override to the new row.
			if err := s.pools.DeleteOverride(ctx, definition.ID, target.Instrument.ID); err != nil {
				return Member{}, err
			}
			if _, err := s.pools.UpsertOverride(ctx, Override{
				PoolID:       definition.ID,
				InstrumentID: stored.ID,
				Action:       OverrideActionAdd,
			}); err != nil {
				return Member{}, err
			}
		}
		target.Instrument = stored
		target.UpdatedAt = stored.UpdatedAt
	}

	if input.ResetName || nameInput != "" {
		alias := nameInput
		if input.ResetName || nameInput == strings.TrimSpace(target.Instrument.Name) {
			alias = ""
		}
		stored, err := s.instruments.SetDisplayName(ctx, target.Instrument.ID, alias)
		if err != nil {
			return Member{}, err
		}
		target.Instrument = stored
		target.UpdatedAt = stored.UpdatedAt
	}

	if target.Source != MemberSourceUser && symbolInput != "" {
		// Merge with any existing symbol overlay so a later name-only edit keeps
		// a previously edited ticker. Name aliases live on the instrument now.
		existing, _, err := s.pools.GetEdit(ctx, definition.ID, target.Instrument.ID)
		if err != nil {
			return Member{}, err
		}
		edit, err := s.pools.UpsertEdit(ctx, Edit{
			PoolID:       definition.ID,
			InstrumentID: target.Instrument.ID,
			Symbol:       firstNonEmpty(symbolInput, existing.Symbol),
			Name:         existing.Name,
		})
		if err != nil {
			return Member{}, err
		}
		target.Instrument = applyMemberEditValue(target.Instrument, edit)
		target.UpdatedAt = edit.UpdatedAt
	}

	return *target, nil
}

// applyMemberEdit overlays a stored edit onto a built-in member's instrument.
func (s *Service) applyMemberEdit(
	ctx context.Context,
	poolID string,
	instrumentID string,
	value instrument.Instrument,
	fallbackUpdatedAt time.Time,
) (instrument.Instrument, time.Time, error) {
	edit, found, err := s.pools.GetEdit(ctx, poolID, instrumentID)
	if err != nil {
		return instrument.Instrument{}, time.Time{}, err
	}
	if !found {
		return value, fallbackUpdatedAt, nil
	}
	return applyMemberEditValue(value, edit), edit.UpdatedAt, nil
}

func applyMemberEditValue(value instrument.Instrument, edit Edit) instrument.Instrument {
	if edit.Symbol != "" {
		value.Symbol = edit.Symbol
	}
	return value
}

func firstNonEmpty(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func (s *Service) requirePool(ctx context.Context, poolID string) (Pool, error) {
	poolID = strings.TrimSpace(poolID)
	value, found, err := s.pools.Get(ctx, poolID)
	if err != nil {
		return Pool{}, err
	}
	if !found {
		return Pool{}, fmt.Errorf("%w: %s", ErrPoolNotFound, poolID)
	}
	return value, nil
}

func (s *Service) requireInstrument(ctx context.Context, instrumentID string) (instrument.Instrument, error) {
	instrumentID = strings.TrimSpace(instrumentID)
	value, found, err := s.instruments.Get(ctx, instrumentID)
	if err != nil {
		return instrument.Instrument{}, err
	}
	if !found {
		return instrument.Instrument{}, fmt.Errorf("%w: %s", ErrInstrumentNotFound, instrumentID)
	}
	return value, nil
}

func (s *Service) isBuiltInMember(ctx context.Context, poolID, instrumentID string) (bool, error) {
	ids, err := s.pools.ListBuiltInMemberIDs(ctx, poolID)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == instrumentID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) ready() error {
	if s == nil || s.instruments == nil || s.pools == nil {
		return errors.New("pool service is not configured")
	}
	return nil
}

func poolAcceptsInstrument(definition Pool, value instrument.Instrument) bool {
	if definition.AssetClass != value.AssetClass {
		return false
	}
	switch definition.Market {
	case "CN-A":
		return value.Market == "CN-A" || value.Market == "CN-GEM" || value.Market == "CN-STAR" ||
			value.Market == "CN-BJ"
	case "HK-MAIN":
		return value.Market == "HK-MAIN" || value.Market == "HK-GEM"
	default:
		return definition.Market == value.Market
	}
}
