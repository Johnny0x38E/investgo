package hot

import "testing"

func TestIsLikelyUSExchange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		exchange, exchDisp string
		want               bool
	}{
		{"NMS", "", true},
		{"NYQ", "", true},
		{"NGM", "", true},
		{"NCM", "", true},
		{"ASE", "", true},
		{"PCX", "NYSEArca", true},
		{"BTS", "BATS Trading", true},
		{"NMS", "NASDAQ", true},
		{"NYQ", "NYSE", true},
		{"PNK", "OTC Markets", false},
		{"LSE", "London", false},
		{"TOR", "Toronto", false},
		{"", "", true},
	}
	for _, tt := range tests {
		if got := isLikelyUSExchange(tt.exchange, tt.exchDisp); got != tt.want {
			t.Fatalf("isLikelyUSExchange(%q, %q) = %v, want %v", tt.exchange, tt.exchDisp, got, tt.want)
		}
	}
}

func TestNormaliseYahooSearchKeyword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"BRK.B", "BRK-B"},
		{"brk.b", "BRK-B"},
		{"BRK-B", "BRK-B"},
		{"BF.B", "BF-B"},
		{"AAPL", "AAPL"},
		{"SPY", "SPY"},
		{"BRK.B.TO", "BRK.B.TO"},
	}
	for _, tt := range tests {
		if got := normaliseYahooSearchKeyword(tt.in); got != tt.want {
			t.Fatalf("normaliseYahooSearchKeyword(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
