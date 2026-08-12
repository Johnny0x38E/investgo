package hot

import (
	"testing"

	"investgo/internal/core"
)

func TestSourceCanRank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sourceID string
		category core.HotCategory
		want     bool
	}{
		{name: "eastmoney CN-A", sourceID: "eastmoney", category: core.HotCategoryCNA, want: true},
		{name: "eastmoney HK", sourceID: "eastmoney", category: core.HotCategoryHK, want: true},
		{name: "eastmoney CN-ETF", sourceID: "eastmoney", category: core.HotCategoryCNETF, want: false},
		{name: "eastmoney US", sourceID: "eastmoney", category: core.HotCategoryUSSP500, want: false},
		{name: "sina CN-A", sourceID: "sina", category: core.HotCategoryCNA, want: true},
		{name: "sina CN-ETF", sourceID: "sina", category: core.HotCategoryCNETF, want: true},
		{name: "sina HK", sourceID: "sina", category: core.HotCategoryHK, want: false},
		{name: "xueqiu CN-A", sourceID: "xueqiu", category: core.HotCategoryCNA, want: true},
		{name: "xueqiu HK", sourceID: "xueqiu", category: core.HotCategoryHK, want: true},
		{name: "xueqiu HK-ETF", sourceID: "xueqiu", category: core.HotCategoryHKETF, want: true},
		{name: "yahoo never ranks", sourceID: "yahoo", category: core.HotCategoryCNA, want: false},
		{name: "finnhub never ranks", sourceID: "finnhub", category: core.HotCategoryUSSP500, want: false},
		{name: "tiingo never ranks", sourceID: "tiingo", category: core.HotCategoryUSSP500, want: false},
		{name: "tencent never ranks", sourceID: "tencent", category: core.HotCategoryCNA, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sourceCanRank(tt.sourceID, tt.category); got != tt.want {
				t.Fatalf("sourceCanRank(%q, %q) = %v; want %v", tt.sourceID, tt.category, got, tt.want)
			}
		})
	}
}

func TestDefaultMembershipSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		category core.HotCategory
		want     string
	}{
		{category: core.HotCategoryCNA, want: "sina"},
		{category: core.HotCategoryCNETF, want: "sina"},
		{category: core.HotCategoryHK, want: "xueqiu"},
		{category: core.HotCategoryHKETF, want: "xueqiu"},
		{category: core.HotCategoryUSSP500, want: ""},
		{category: core.HotCategoryUSNasdaq, want: ""},
		{category: core.HotCategoryUSDow, want: ""},
		{category: core.HotCategoryUSETF, want: ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			t.Parallel()
			if got := defaultMembershipSource(tt.category); got != tt.want {
				t.Fatalf("defaultMembershipSource(%q) = %q; want %q", tt.category, got, tt.want)
			}
		})
	}
}

func TestResolveMembershipSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		category  core.HotCategory
		quoteSrc  string
		want      string
	}{
		{name: "CN prefers eastmoney when it can rank", category: core.HotCategoryCNA, quoteSrc: "eastmoney", want: "eastmoney"},
		{name: "CN falls back to sina for yahoo", category: core.HotCategoryCNA, quoteSrc: "yahoo", want: "sina"},
		{name: "CN-ETF falls back for eastmoney", category: core.HotCategoryCNETF, quoteSrc: "eastmoney", want: "sina"},
		{name: "CN-ETF keeps sina", category: core.HotCategoryCNETF, quoteSrc: "sina", want: "sina"},
		{name: "HK prefers eastmoney", category: core.HotCategoryHK, quoteSrc: "eastmoney", want: "eastmoney"},
		{name: "HK falls back to xueqiu for tencent", category: core.HotCategoryHK, quoteSrc: "tencent", want: "xueqiu"},
		{name: "US pool has no membership source", category: core.HotCategoryUSSP500, quoteSrc: "yahoo", want: ""},
		{name: "HKETF pool has no ranking membership", category: core.HotCategoryHKETF, quoteSrc: "xueqiu", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveMembershipSource(tt.category, tt.quoteSrc); got != tt.want {
				t.Fatalf("resolveMembershipSource(%q, %q) = %q; want %q", tt.category, tt.quoteSrc, got, tt.want)
			}
		})
	}
}

func TestIsPoolCategory(t *testing.T) {
	t.Parallel()

	if !isPoolCategory(core.HotCategoryUSSP500) {
		t.Fatal("US SP500 should be pool-backed")
	}
	if !isPoolCategory(core.HotCategoryHKETF) {
		t.Fatal("HK ETF should be pool-backed")
	}
	if isPoolCategory(core.HotCategoryCNA) {
		t.Fatal("CN-A should not be pool-backed")
	}
	if isPoolCategory(core.HotCategoryHK) {
		t.Fatal("HK main should not be pool-backed")
	}
}
