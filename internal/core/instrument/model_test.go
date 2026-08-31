package instrument

import (
	"errors"
	"testing"
)

func TestNormalizeCanonicalInstrumentIdentities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input Instrument
		want  Instrument
	}{
		{
			name: "Shanghai equity",
			input: Instrument{
				AssetClass:    AssetClassEquity,
				Symbol:        " sh600519 ",
				Name:          " 贵州茅台 ",
				Market:        "cn",
				QuoteCurrency: "cny",
			},
			want: Instrument{
				AssetClass:    AssetClassEquity,
				Symbol:        "600519",
				Name:          "贵州茅台",
				Market:        "CN-A",
				Exchange:      "SSE",
				QuoteCurrency: "CNY",
				Status:        InstrumentStatusActive,
			},
		},
		{
			name: "Shenzhen ETF",
			input: Instrument{
				AssetClass: AssetClassETF,
				Symbol:     "159915.sz",
				Name:       "创业板 ETF",
				Market:     "cn-etf",
			},
			want: Instrument{
				AssetClass:    AssetClassETF,
				Symbol:        "159915",
				Name:          "创业板 ETF",
				Market:        "CN-ETF",
				Exchange:      "SZSE",
				QuoteCurrency: "CNY",
				Status:        InstrumentStatusActive,
			},
		},
		{
			name: "Hong Kong equity",
			input: Instrument{
				AssetClass: AssetClassEquity,
				Symbol:     "700.hk",
				Name:       "Tencent",
				Market:     "hk",
			},
			want: Instrument{
				AssetClass:    AssetClassEquity,
				Symbol:        "00700",
				Name:          "Tencent",
				Market:        "HK-MAIN",
				Exchange:      "HKEX",
				QuoteCurrency: "HKD",
				Status:        InstrumentStatusActive,
			},
		},
		{
			name: "US equity with class share separator",
			input: Instrument{
				AssetClass:    AssetClassEquity,
				Symbol:        " brk.b ",
				Name:          "Berkshire Hathaway",
				Market:        "us",
				Exchange:      "nyq",
				QuoteCurrency: "usd",
			},
			want: Instrument{
				AssetClass:    AssetClassEquity,
				Symbol:        "BRK-B",
				Name:          "Berkshire Hathaway",
				Market:        "US-STOCK",
				Exchange:      "NYSE",
				QuoteCurrency: "USD",
				Status:        InstrumentStatusActive,
			},
		},
		{
			name: "US ETF",
			input: Instrument{
				AssetClass: AssetClassETF,
				Symbol:     " spy ",
				Name:       "SPDR S&P 500 ETF Trust",
				Market:     "us etf",
				Exchange:   "arcx",
			},
			want: Instrument{
				AssetClass:    AssetClassETF,
				Symbol:        "SPY",
				Name:          "SPDR S&P 500 ETF Trust",
				Market:        "US-ETF",
				Exchange:      "NYSEARCA",
				QuoteCurrency: "USD",
				Status:        InstrumentStatusActive,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Normalize(tt.input)
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			assertInstrumentIdentity(t, got, tt.want)
			if got.Name != tt.want.Name || got.QuoteCurrency != tt.want.QuoteCurrency || got.Status != tt.want.Status {
				t.Fatalf("Normalize() metadata = (%q, %q, %q), want (%q, %q, %q)", got.Name, got.QuoteCurrency, got.Status, tt.want.Name, tt.want.QuoteCurrency, tt.want.Status)
			}
		})
	}
}

func TestEquivalentInputsHaveOneCanonicalIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  Instrument
		right Instrument
	}{
		{
			name:  "Shanghai prefixes and suffixes",
			left:  Instrument{AssetClass: AssetClassEquity, Market: "CN-A", Symbol: "SH600519"},
			right: Instrument{AssetClass: AssetClassEquity, Market: "a-share", Exchange: "XSHG", Symbol: "600519.SH"},
		},
		{
			name:  "Hong Kong padding",
			left:  Instrument{AssetClass: AssetClassEquity, Market: "HK", Symbol: "700"},
			right: Instrument{AssetClass: AssetClassEquity, Market: "HK-MAIN", Exchange: "HKEX", Symbol: "00700.HK"},
		},
		{
			name:  "US exchange and class aliases",
			left:  Instrument{AssetClass: AssetClassEquity, Market: "US", Exchange: "NYQ", Symbol: "brk.b"},
			right: Instrument{AssetClass: AssetClassEquity, Market: "US-STOCK", Exchange: "NYSE", Symbol: "BRK-B"},
		},
		{
			name:  "US ETF market and exchange aliases",
			left:  Instrument{AssetClass: AssetClassETF, Market: "ETF", Exchange: "ARCX", Symbol: "spy"},
			right: Instrument{AssetClass: AssetClassETF, Market: "US-ETF", Exchange: "NYSEARCA", Symbol: "SPY"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			left, err := Normalize(tt.left)
			if err != nil {
				t.Fatalf("Normalize(left) error = %v", err)
			}
			right, err := Normalize(tt.right)
			if err != nil {
				t.Fatalf("Normalize(right) error = %v", err)
			}

			if left.Identity() != right.Identity() {
				t.Fatalf("identities differ: left=%+v right=%+v", left.Identity(), right.Identity())
			}
			leftID, err := CanonicalID(left.Identity())
			if err != nil {
				t.Fatalf("CanonicalID(left) error = %v", err)
			}
			rightID, err := CanonicalID(right.Identity())
			if err != nil {
				t.Fatalf("CanonicalID(right) error = %v", err)
			}
			if leftID != rightID {
				t.Fatalf("canonical IDs differ: left=%q right=%q", leftID, rightID)
			}
		})
	}
}

func TestNormalizeRejectsInvalidInstrumentIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input Instrument
		field string
	}{
		{name: "unknown asset class", input: Instrument{AssetClass: "bond", Market: "US-STOCK", Symbol: "AAPL"}, field: "assetClass"},
		{name: "asset class conflicts with market", input: Instrument{AssetClass: AssetClassETF, Market: "US-STOCK", Symbol: "SPY"}, field: "market"},
		{name: "invalid A-share symbol", input: Instrument{AssetClass: AssetClassEquity, Market: "CN-A", Symbol: "60051"}, field: "symbol"},
		{name: "invalid Hong Kong symbol", input: Instrument{AssetClass: AssetClassEquity, Market: "HK-MAIN", Symbol: "TENCENT"}, field: "symbol"},
		{name: "missing US symbol", input: Instrument{AssetClass: AssetClassEquity, Market: "US-STOCK"}, field: "symbol"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Normalize(tt.input)
			if err == nil {
				t.Fatal("Normalize() error = nil")
			}
			if !errors.Is(err, ErrInvalidInstrument) {
				t.Fatalf("Normalize() error = %v, want ErrInvalidInstrument", err)
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("Normalize() error type = %T, want *ValidationError", err)
			}
			if validationErr.Field != tt.field {
				t.Fatalf("ValidationError.Field = %q, want %q", validationErr.Field, tt.field)
			}
		})
	}
}

func assertInstrumentIdentity(t *testing.T, got, want Instrument) {
	t.Helper()

	if got.Identity() != want.Identity() {
		t.Fatalf("identity = %+v, want %+v", got.Identity(), want.Identity())
	}
}
