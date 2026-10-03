package logger

import (
	"regexp"
	"strings"
)

// sensitiveLogPatterns mask provider credentials before they reach the log book.
// Client logs and store logs share this list so the API does not trust a caller
// that claims to have redacted already.
var sensitiveLogPatterns = []*regexp.Regexp{
	regexp.MustCompile(
		`(?i)(alphaVantageApiKey|twelveDataApiKey|finnhubApiKey|tiingoApiKey|polygonApiKey)\s*[:=]\s*["']?[^"'\s,;]+["']?`,
	),
	regexp.MustCompile(`(?i)(apikey|api_key|key)=([^&\s]+)`),
}

// RedactSensitiveText replaces known API-key assignments with a masked value.
func RedactSensitiveText(message string) string {
	redacted := message
	for _, pattern := range sensitiveLogPatterns {
		redacted = pattern.ReplaceAllStringFunc(redacted, func(segment string) string {
			if strings.Contains(segment, "=") {
				parts := strings.SplitN(segment, "=", 2)
				return parts[0] + "=***"
			}
			if strings.Contains(segment, ":") {
				parts := strings.SplitN(segment, ":", 2)
				return parts[0] + ": ***"
			}
			return "***"
		})
	}
	return redacted
}
