package pool_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"investgo/internal/core/instrument"
	"investgo/internal/core/pool"
	sqlitestorage "investgo/internal/storage/sqlite"
)

func TestServiceExclusionIsPoolScopedAndDoesNotDeleteWatchlist(t *testing.T) {
	t.Parallel()

	ctx, db, catalog, pools, service, instruments := newPoolServiceFixture(t)
	aapl := instruments["AAPL"]

	if _, err := db.ExecContext(ctx, `
		INSERT INTO watchlist_entries(id, instrument_id, updated_at)
		VALUES ('watch-aapl', ?, '2026-08-31T00:00:00Z')
	`, aapl.ID); err != nil {
		t.Fatalf("insert watchlist entry: %v", err)
	}

	excluded, err := service.ExcludeMember(ctx, pool.PoolIDUSSP500, aapl.ID)
	if err != nil {
		t.Fatalf("ExcludeMember() error = %v", err)
	}
	if excluded.Source != pool.MemberSourceBuiltIn || excluded.Status != pool.MemberStatusExcluded {
		t.Fatalf("excluded member = %+v", excluded)
	}

	sp500, err := service.EffectiveMembers(ctx, pool.PoolIDUSSP500)
	if err != nil {
		t.Fatalf("EffectiveMembers(S&P 500) error = %v", err)
	}
	assertInstrumentSymbols(t, sp500, []string{})
	nasdaq, err := service.EffectiveMembers(ctx, pool.PoolIDUSNasdaq)
	if err != nil {
		t.Fatalf("EffectiveMembers(Nasdaq) error = %v", err)
	}
	assertInstrumentSymbols(t, nasdaq, []string{"AAPL", "MSFT"})

	var watchlistCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM watchlist_entries WHERE id = 'watch-aapl'").Scan(&watchlistCount); err != nil {
		t.Fatalf("count watchlist entries: %v", err)
	}
	if watchlistCount != 1 {
		t.Fatalf("watchlist entry count = %d, want 1", watchlistCount)
	}

	restarted := pool.NewService(catalog, pools)
	members, err := restarted.ListMembers(ctx, pool.PoolIDUSSP500)
	if err != nil {
		t.Fatalf("ListMembers(after restart) error = %v", err)
	}
	if len(members) != 1 || members[0].Status != pool.MemberStatusExcluded {
		t.Fatalf("members after restart = %+v", members)
	}

	restored, err := restarted.RestoreMember(ctx, pool.PoolIDUSSP500, aapl.ID)
	if err != nil {
		t.Fatalf("RestoreMember() error = %v", err)
	}
	if restored.Status != pool.MemberStatusActive || restored.Source != pool.MemberSourceBuiltIn {
		t.Fatalf("restored member = %+v", restored)
	}
	sp500, err = restarted.EffectiveMembers(ctx, pool.PoolIDUSSP500)
	if err != nil {
		t.Fatalf("EffectiveMembers(restored) error = %v", err)
	}
	assertInstrumentSymbols(t, sp500, []string{"AAPL"})
}

type failingEditRepository struct {
	pool.Repository
}

func (r failingEditRepository) GetEdit(context.Context, string, string) (pool.Edit, bool, error) {
	return pool.Edit{}, false, errEditLookup
}

var errEditLookup = errors.New("edit lookup failed")

func TestServiceListMembersSurfacesEditLookupErrors(t *testing.T) {
	t.Parallel()

	ctx, _, catalog, pools, _, _ := newPoolServiceFixture(t)
	service := pool.NewService(catalog, failingEditRepository{Repository: pools})
	if _, err := service.ListMembers(ctx, pool.PoolIDUSSP500); !errors.Is(err, errEditLookup) {
		t.Fatalf("ListMembers() error = %v, want %v", err, errEditLookup)
	}
}

func TestServiceBaselineUpgradePromotesUserAdditionsAndDropsGhostExclusions(t *testing.T) {
	t.Parallel()

	ctx, _, catalog, pools, service, instruments := newPoolServiceFixture(t)
	added, err := service.AddMember(ctx, pool.PoolIDUSSP500, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Symbol:     "NVDA",
		Name:       "NVIDIA",
	})
	if err != nil {
		t.Fatalf("AddMember(NVDA) error = %v", err)
	}
	ghost, err := catalog.Upsert(ctx, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Symbol:     "GOOG",
		Name:       "Alphabet",
	})
	if err != nil {
		t.Fatalf("upsert GOOG: %v", err)
	}
	if _, err := service.ExcludeMember(ctx, pool.PoolIDUSSP500, ghost.ID); err != nil {
		t.Fatalf("ExcludeMember(GOOG) error = %v", err)
	}

	aapl := instruments["AAPL"]
	if err := pools.ReplaceBuiltInMembers(ctx, pool.PoolIDUSSP500, "v2", []string{aapl.ID, added.Instrument.ID}); err != nil {
		t.Fatalf("replace baseline: %v", err)
	}

	members, err := pool.NewService(catalog, pools).ListMembers(ctx, pool.PoolIDUSSP500)
	if err != nil {
		t.Fatalf("ListMembers() error = %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("members = %+v, want AAPL and NVDA", members)
	}
	for _, member := range members {
		if member.Source != pool.MemberSourceBuiltIn || member.Status != pool.MemberStatusActive {
			t.Fatalf("member %s = %+v, want builtin/active", member.Instrument.Symbol, member)
		}
		if member.Instrument.Symbol == "GOOG" {
			t.Fatal("ghost GOOG exclusion survived baseline upgrade")
		}
	}
	if override, found, err := pools.GetOverride(ctx, pool.PoolIDUSSP500, added.Instrument.ID); err != nil || found {
		t.Fatalf("NVDA add override = %+v found %v error %v; want deleted", override, found, err)
	}
	if _, err := pool.NewService(catalog, pools).ExcludeMember(ctx, pool.PoolIDUSSP500, added.Instrument.ID); err != nil {
		t.Fatalf("ExcludeMember(promoted NVDA) error = %v", err)
	}
}

func TestServiceUserAdditionPersistsAndDeleteRemovesAddOverride(t *testing.T) {
	t.Parallel()

	ctx, _, catalog, pools, service, _ := newPoolServiceFixture(t)
	added, err := service.AddMember(ctx, pool.PoolIDUSNasdaq, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US",
		Exchange:   "NASDAQ",
		Symbol:     "NVDA",
		Name:       "NVIDIA",
	})
	if err != nil {
		t.Fatalf("AddMember() error = %v", err)
	}
	if added.Source != pool.MemberSourceUser || added.Status != pool.MemberStatusActive {
		t.Fatalf("added member = %+v", added)
	}

	if _, err := service.AddMember(ctx, pool.PoolIDUSNasdaq, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Exchange:   "NMS",
		Symbol:     "nvda",
		Name:       "NVIDIA Corporation",
	}); err != nil {
		t.Fatalf("AddMember(duplicate canonical identity) error = %v", err)
	}

	restarted := pool.NewService(catalog, pools)
	members, err := restarted.ListMembers(ctx, pool.PoolIDUSNasdaq)
	if err != nil {
		t.Fatalf("ListMembers(after restart) error = %v", err)
	}
	var nvda *pool.Member
	for index := range members {
		if members[index].Instrument.Symbol == "NVDA" {
			nvda = &members[index]
			break
		}
	}
	if nvda == nil || nvda.Source != pool.MemberSourceUser {
		t.Fatalf("members after restart = %+v", members)
	}

	if err := restarted.DeleteUserMember(ctx, pool.PoolIDUSNasdaq, nvda.Instrument.ID); err != nil {
		t.Fatalf("DeleteUserMember() error = %v", err)
	}
	if _, found, err := pools.GetOverride(ctx, pool.PoolIDUSNasdaq, nvda.Instrument.ID); err != nil || found {
		t.Fatalf("override after delete = found %v, error %v", found, err)
	}
	members, err = restarted.ListMembers(ctx, pool.PoolIDUSNasdaq)
	if err != nil {
		t.Fatalf("ListMembers(after delete) error = %v", err)
	}
	for _, member := range members {
		if member.Instrument.Symbol == "NVDA" {
			t.Fatalf("members after delete = %+v, NVDA still present", members)
		}
	}
}

func TestServiceDeletingUserMemberNeverCreatesExclusion(t *testing.T) {
	t.Parallel()

	ctx, _, _, pools, service, _ := newPoolServiceFixture(t)
	added, err := service.AddMember(ctx, pool.PoolIDUSSP500, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Exchange:   "NASDAQ",
		Symbol:     "NVDA",
		Name:       "NVIDIA",
	})
	if err != nil {
		t.Fatalf("AddMember() error = %v", err)
	}
	if err := service.DeleteUserMember(ctx, pool.PoolIDUSSP500, added.Instrument.ID); err != nil {
		t.Fatalf("DeleteUserMember() error = %v", err)
	}
	if override, found, err := pools.GetOverride(ctx, pool.PoolIDUSSP500, added.Instrument.ID); err != nil || found {
		t.Fatalf("override after delete = %+v, found %v, error %v", override, found, err)
	}
}

func TestServiceUpdateMemberEditsBuiltInMemberPersistently(t *testing.T) {
	t.Parallel()

	ctx, _, catalog, pools, service, instruments := newPoolServiceFixture(t)
	aapl := instruments["AAPL"]

	edited, err := service.UpdateMember(ctx, pool.PoolIDUSSP500, aapl.ID, pool.UpdateMemberInput{
		Name: "Apple Inc.",
	})
	if err != nil {
		t.Fatalf("UpdateMember(name) error = %v", err)
	}
	if edited.Instrument.Name != "Apple Inc." || edited.Instrument.Symbol != "AAPL" {
		t.Fatalf("edited member = %+v", edited)
	}
	if edited.Source != pool.MemberSourceBuiltIn {
		t.Fatalf("edited member source = %q, want builtin", edited.Source)
	}

	// Symbol rename on top of the name edit.
	edited, err = service.UpdateMember(ctx, pool.PoolIDUSSP500, aapl.ID, pool.UpdateMemberInput{
		Symbol: "META",
	})
	if err != nil {
		t.Fatalf("UpdateMember(symbol) error = %v", err)
	}
	if edited.Instrument.Symbol != "META" || edited.Instrument.Name != "Apple Inc." {
		t.Fatalf("edited member = %+v", edited)
	}

	// The shipped baseline row must stay untouched.
	baseline, found, err := catalog.Get(ctx, aapl.ID)
	if err != nil || !found {
		t.Fatalf("baseline Get = found %v, error %v", found, err)
	}
	if baseline.Symbol != "AAPL" || baseline.Name != "Apple" {
		t.Fatalf("baseline instrument mutated: %+v", baseline)
	}

	// Restart + data-version bump: the edit persists and the baseline is intact.
	if err := pools.ReplaceBuiltInMembers(ctx, pool.PoolIDUSSP500, "v2", []string{aapl.ID}); err != nil {
		t.Fatalf("replace baseline: %v", err)
	}
	restarted := pool.NewService(catalog, pools)
	members, err := restarted.EffectiveMembers(ctx, pool.PoolIDUSSP500)
	if err != nil {
		t.Fatalf("EffectiveMembers(after restart) error = %v", err)
	}
	assertInstrumentSymbols(t, members, []string{"META"})
	if members[0].Name != "Apple Inc." {
		t.Fatalf("member name after restart = %q, want Apple Inc.", members[0].Name)
	}

	// Exclude then restore: the edited display data survives.
	if _, err := restarted.ExcludeMember(ctx, pool.PoolIDUSSP500, aapl.ID); err != nil {
		t.Fatalf("ExcludeMember() error = %v", err)
	}
	if _, err := restarted.RestoreMember(ctx, pool.PoolIDUSSP500, aapl.ID); err != nil {
		t.Fatalf("RestoreMember() error = %v", err)
	}
	members, err = restarted.EffectiveMembers(ctx, pool.PoolIDUSSP500)
	if err != nil {
		t.Fatalf("EffectiveMembers(after restore) error = %v", err)
	}
	assertInstrumentSymbols(t, members, []string{"META"})
}

func TestServiceUpdateMemberEditsUserMemberAndMovesOverride(t *testing.T) {
	t.Parallel()

	ctx, _, catalog, pools, service, _ := newPoolServiceFixture(t)
	added, err := service.AddMember(ctx, pool.PoolIDUSNasdaq, instrument.Instrument{
		AssetClass: instrument.AssetClassEquity,
		Market:     "US-STOCK",
		Exchange:   "NASDAQ",
		Symbol:     "NVDA",
		Name:       "NVIDIA",
	})
	if err != nil {
		t.Fatalf("AddMember() error = %v", err)
	}

	edited, err := service.UpdateMember(ctx, pool.PoolIDUSNasdaq, added.Instrument.ID, pool.UpdateMemberInput{
		Symbol: "NVDA",
		Name:   "NVIDIA Corporation",
	})
	if err != nil {
		t.Fatalf("UpdateMember(name) error = %v", err)
	}
	if edited.Instrument.Name != "NVIDIA Corporation" {
		t.Fatalf("edited member = %+v", edited)
	}

	edited, err = service.UpdateMember(ctx, pool.PoolIDUSNasdaq, edited.Instrument.ID, pool.UpdateMemberInput{
		Symbol: "NVDA2",
	})
	if err != nil {
		t.Fatalf("UpdateMember(symbol) error = %v", err)
	}
	if edited.Instrument.Symbol != "NVDA2" {
		t.Fatalf("edited member = %+v", edited)
	}
	if override, found, err := pools.GetOverride(ctx, pool.PoolIDUSNasdaq, edited.Instrument.ID); err != nil || !found || override.Action != pool.OverrideActionAdd {
		t.Fatalf("override after edit = %+v, found %v, error %v", override, found, err)
	}
	if override, found, err := pools.GetOverride(ctx, pool.PoolIDUSNasdaq, added.Instrument.ID); err != nil || found {
		t.Fatalf("old override after edit = %+v, found %v, error %v", override, found, err)
	}

	restarted := pool.NewService(catalog, pools)
	members, err := restarted.EffectiveMembers(ctx, pool.PoolIDUSNasdaq)
	if err != nil {
		t.Fatalf("EffectiveMembers(after restart) error = %v", err)
	}
	assertInstrumentSymbols(t, members, []string{"AAPL", "MSFT", "NVDA2"})
}

func TestServiceUpdateMemberRejectsCollisionAndExcludedMember(t *testing.T) {
	t.Parallel()

	ctx, _, _, _, service, instruments := newPoolServiceFixture(t)
	aapl := instruments["AAPL"]

	// Renaming a built-in member to another member's symbol must fail.
	if _, err := service.UpdateMember(ctx, pool.PoolIDUSNasdaq, aapl.ID, pool.UpdateMemberInput{
		Symbol: "MSFT",
	}); err == nil {
		t.Fatal("UpdateMember(collision) error = nil; want duplicate error")
	}

	// Excluded members cannot be edited until restored.
	if _, err := service.ExcludeMember(ctx, pool.PoolIDUSSP500, aapl.ID); err != nil {
		t.Fatalf("ExcludeMember() error = %v", err)
	}
	if _, err := service.UpdateMember(ctx, pool.PoolIDUSSP500, aapl.ID, pool.UpdateMemberInput{
		Name: "Apple Inc.",
	}); err == nil {
		t.Fatal("UpdateMember(excluded) error = nil; want invalid operation")
	}
}

func newPoolServiceFixture(t *testing.T) (
	context.Context,
	*sql.DB,
	instrument.Repository,
	pool.Repository,
	*pool.Service,
	map[string]instrument.Instrument,
) {
	t.Helper()

	ctx := context.Background()
	db, err := sqlitestorage.Open(filepath.Join(t.TempDir(), "investgo.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestorage.ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	catalog := sqlitestorage.NewInstrumentRepository(db)
	pools := sqlitestorage.NewPoolRepository(db)

	definitions := []pool.Pool{
		{ID: pool.PoolIDUSSP500, Name: "S&P 500", Market: "US-STOCK", AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex},
		{ID: pool.PoolIDUSNasdaq, Name: "Nasdaq 100", Market: "US-STOCK", AssetClass: instrument.AssetClassEquity, Type: pool.TypeIndex},
	}
	for _, definition := range definitions {
		if _, err := pools.Upsert(ctx, definition); err != nil {
			t.Fatalf("upsert pool %s: %v", definition.ID, err)
		}
	}

	instruments := make(map[string]instrument.Instrument)
	for _, value := range []instrument.Instrument{
		{AssetClass: instrument.AssetClassEquity, Market: "US-STOCK", Symbol: "AAPL", Name: "Apple"},
		{AssetClass: instrument.AssetClassEquity, Market: "US-STOCK", Symbol: "MSFT", Name: "Microsoft"},
	} {
		stored, err := catalog.Upsert(ctx, value)
		if err != nil {
			t.Fatalf("upsert instrument %s: %v", value.Symbol, err)
		}
		instruments[stored.Symbol] = stored
	}
	if err := pools.ReplaceBuiltInMembers(ctx, pool.PoolIDUSSP500, "v1", []string{instruments["AAPL"].ID}); err != nil {
		t.Fatalf("seed S&P 500: %v", err)
	}
	if err := pools.ReplaceBuiltInMembers(ctx, pool.PoolIDUSNasdaq, "v1", []string{instruments["AAPL"].ID, instruments["MSFT"].ID}); err != nil {
		t.Fatalf("seed Nasdaq: %v", err)
	}
	return ctx, db, catalog, pools, pool.NewService(catalog, pools), instruments
}

func assertInstrumentSymbols(t *testing.T, values []instrument.Instrument, want []string) {
	t.Helper()

	got := make([]string, len(values))
	for index, value := range values {
		got[index] = value.Symbol
	}
	if !slices.Equal(got, want) {
		t.Fatalf("symbols = %v, want %v", got, want)
	}
}
