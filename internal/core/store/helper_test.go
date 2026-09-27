package store

import (
	"testing"

	"investgo/internal/core"
)

func TestApplyQuoteToItemKeepsCustomDisplayName(t *testing.T) {
	t.Parallel()

	item := core.WatchlistItem{
		Symbol:        "AAPL",
		Name:          "苹果",
		DefaultName:   "Apple",
		HasCustomName: true,
	}
	applyQuoteToItem(&item, core.Quote{Name: "Apple Inc.", CurrentPrice: 190})
	if item.Name != "苹果" {
		t.Fatalf("custom name overwritten: %q", item.Name)
	}
	if item.DefaultName != "Apple Inc." {
		t.Fatalf("default name = %q, want Apple Inc.", item.DefaultName)
	}
	if item.CurrentPrice != 190 {
		t.Fatalf("current price = %v, want 190", item.CurrentPrice)
	}
}

func TestResolveSavedDisplayMarksRequestedAlias(t *testing.T) {
	t.Parallel()

	item := core.WatchlistItem{Symbol: "AAPL", Name: "Apple Inc.", HasCustomName: true}
	existing := core.WatchlistItem{Symbol: "AAPL", Name: "Apple", DefaultName: "Apple"}
	resolveSavedDisplay(&item, &existing, "苹果")
	if !item.HasCustomName || item.Name != "苹果" || item.DefaultName != "Apple Inc." {
		t.Fatalf("resolved = %+v", item)
	}

	item = core.WatchlistItem{Symbol: "AAPL", Name: "Apple Inc."}
	resolveSavedDisplay(&item, &existing, "Apple")
	if item.HasCustomName || item.Name != "Apple Inc." {
		t.Fatalf("reset resolved = %+v", item)
	}
}
