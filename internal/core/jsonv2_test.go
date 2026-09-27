package core

import (
	json "encoding/json/v2"
	"strings"
	"testing"
	"time"
)

func TestDCAEntryOmitsZeroAmountsThatMeanNotFilled(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(DCAEntry{
		ID:     "entry-1",
		Date:   time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		Amount: 100,
		Shares: 2,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	encoded := string(payload)
	for _, field := range []string{`"price"`, `"fee"`, `"effectivePrice"`, `"note"`} {
		if strings.Contains(encoded, field) {
			t.Fatalf("Marshal() = %s, unexpectedly contains %s", encoded, field)
		}
	}

	payload, err = json.Marshal(DCAEntry{
		ID:             "entry-2",
		Date:           time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		Amount:         100,
		Shares:         2,
		Price:          50,
		Fee:            1.5,
		EffectivePrice: 50.75,
		Note:           "open",
	})
	if err != nil {
		t.Fatalf("Marshal() filled entry error = %v", err)
	}
	encoded = string(payload)
	for _, field := range []string{`"price":50`, `"fee":1.5`, `"effectivePrice":50.75`, `"note":"open"`} {
		if !strings.Contains(encoded, field) {
			t.Fatalf("Marshal() = %s, missing %s", encoded, field)
		}
	}
}

func TestNilWatchlistTagsMarshalAsEmptyArray(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(WatchlistItem{Tags: nil})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(payload), `"tags":[]`) {
		t.Fatalf("Marshal() = %s, want tags as []", payload)
	}
}
