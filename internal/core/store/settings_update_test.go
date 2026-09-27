package store

import (
	"testing"
	"time"

	"investgo/internal/core"
	"investgo/internal/logger"
)

func TestUpdateSettingsPartialUpdatePreservesOmittedSecrets(t *testing.T) {
	appStore := newLookupTestStore(t, &lookupStubProvider{name: "Yahoo Finance"})

	secret := "secret-key"
	developerMode := true
	if _, err := appStore.UpdateSettings(SettingsUpdate{
		AlphaVantageAPIKey: &secret,
		DeveloperMode:      &developerMode,
	}); err != nil {
		t.Fatalf("set secret: %v", err)
	}

	theme := "dark"
	if _, err := appStore.UpdateSettings(SettingsUpdate{ThemeMode: &theme}); err != nil {
		t.Fatalf("partial theme update: %v", err)
	}

	settings := appStore.CurrentSettings()
	if settings.AlphaVantageAPIKey != secret {
		t.Fatalf("omitted API key = %q, want it preserved", settings.AlphaVantageAPIKey)
	}
	if settings.ThemeMode != "dark" {
		t.Fatalf("theme = %q, want dark", settings.ThemeMode)
	}
	if !settings.DeveloperMode {
		t.Fatal("omitted developerMode was reset")
	}

	cleared := ""
	if _, err := appStore.UpdateSettings(SettingsUpdate{AlphaVantageAPIKey: &cleared}); err != nil {
		t.Fatalf("clear API key: %v", err)
	}
	if got := appStore.CurrentSettings().AlphaVantageAPIKey; got != "" {
		t.Fatalf("cleared API key = %q, want empty", got)
	}
	if !appStore.CurrentSettings().DeveloperMode {
		t.Fatal("clearing the API key reset developerMode")
	}
}

func TestUpdateSettingsClampsOversizedHotCacheTTL(t *testing.T) {
	appStore := newLookupTestStore(t, &lookupStubProvider{name: "Yahoo Finance"})

	oversized := core.MaxHotCacheTTLSeconds + 1
	if _, err := appStore.UpdateSettings(SettingsUpdate{HotCacheTTLSeconds: &oversized}); err != nil {
		t.Fatalf("oversized TTL: %v", err)
	}
	if got := appStore.CurrentSettings().HotCacheTTLSeconds; got != core.MaxHotCacheTTLSeconds {
		t.Fatalf("hotCacheTTLSeconds = %d, want %d", got, core.MaxHotCacheTTLSeconds)
	}
	if got := appStore.derivedCacheTTL(); got != time.Duration(core.MaxHotCacheTTLSeconds)*time.Second {
		t.Fatalf("derived cache TTL = %s, want %s", got, time.Duration(core.MaxHotCacheTTLSeconds)*time.Second)
	}

	atMax := core.MaxHotCacheTTLSeconds
	if _, err := appStore.UpdateSettings(SettingsUpdate{HotCacheTTLSeconds: &atMax}); err != nil {
		t.Fatalf("max TTL: %v", err)
	}
	if got := appStore.CurrentSettings().HotCacheTTLSeconds; got != atMax {
		t.Fatalf("hotCacheTTLSeconds = %d, want %d", got, atMax)
	}

	tooSmall := core.MinHotCacheTTLSeconds - 1
	if _, err := appStore.UpdateSettings(SettingsUpdate{HotCacheTTLSeconds: &tooSmall}); err == nil {
		t.Fatal("expected TTL below the minimum to be rejected")
	}
	if got := appStore.CurrentSettings().HotCacheTTLSeconds; got != atMax {
		t.Fatalf("rejected update changed TTL to %d, want %d", got, atMax)
	}
}

func TestLoadClampsOversizedHotCacheTTL(t *testing.T) {
	appStore, err := NewStoreWithRepository(
		&seededMemoryRepository{state: PersistedState{
			Settings: core.AppSettings{
				HotCacheTTLSeconds: core.MaxHotCacheTTLSeconds + 5000,
				ProxyMode:          "system",
			},
		}},
		map[string]core.QuoteProvider{
			"sina":   &lookupStubProvider{name: "Sina Finance"},
			"xueqiu": &lookupStubProvider{name: "Xueqiu"},
			"yahoo":  &lookupStubProvider{name: "Yahoo Finance"},
		},
		[]core.QuoteSourceOption{
			{ID: "sina", Name: "Sina Finance"},
			{ID: "xueqiu", Name: "Xueqiu"},
			{ID: "yahoo", Name: "Yahoo Finance"},
		},
		nil,
		logger.NewLogBook(10),
		"test",
		nil,
	)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if got := appStore.CurrentSettings().HotCacheTTLSeconds; got != core.MaxHotCacheTTLSeconds {
		t.Fatalf("loaded hotCacheTTLSeconds = %d, want %d", got, core.MaxHotCacheTTLSeconds)
	}
}
