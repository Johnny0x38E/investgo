package logger

import (
	"strings"
	"testing"
)

func TestRedactSensitiveTextMasksProviderKeys(t *testing.T) {
	input := "alphaVantageApiKey=supersecret twelveDataApiKey: 'other-secret' url?apikey=query-secret"
	redacted := RedactSensitiveText(input)
	for _, secret := range []string{"supersecret", "other-secret", "query-secret"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted log still contains %q: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "alphaVantageApiKey=***") || !strings.Contains(redacted, "apikey=***") {
		t.Fatalf("redacted log = %s", redacted)
	}
}
