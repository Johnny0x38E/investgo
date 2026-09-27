package store

import "testing"

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
