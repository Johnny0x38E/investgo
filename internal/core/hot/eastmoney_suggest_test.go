package hot

import (
	"testing"

	"investgo/internal/core"
)

func TestEastMoneySuggestToSeedFiltersByCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		item     eastMoneySuggestItem
		category core.HotCategory
		wantOK   bool
		wantMkt  string
	}{
		{
			name:     "HK stock stays out of HK ETF pool",
			item:     eastMoneySuggestItem{Code: "00700", Name: "腾讯控股", MktNum: "128", SecurityTypeName: "港股"},
			category: core.HotCategoryHKETF,
			wantOK:   false,
		},
		{
			name:     "HK ETF stays in HK ETF pool",
			item:     eastMoneySuggestItem{Code: "02800", Name: "盈富基金", MktNum: "128", SecurityTypeName: "港股ETF"},
			category: core.HotCategoryHKETF,
			wantOK:   true,
			wantMkt:  "HK-ETF",
		},
		{
			name:     "HK ETF stays out of HK stock pool",
			item:     eastMoneySuggestItem{Code: "02800", Name: "盈富基金", MktNum: "128", SecurityTypeName: "港股ETF"},
			category: core.HotCategoryHK,
			wantOK:   false,
		},
		{
			name:     "A-share stays in CN-A pool",
			item:     eastMoneySuggestItem{Code: "600519", Name: "贵州茅台", MktNum: "1", SecurityTypeName: "A股"},
			category: core.HotCategoryCNA,
			wantOK:   true,
			wantMkt:  "CN-A",
		},
		{
			name:     "onshore ETF stays in CN-ETF pool",
			item:     eastMoneySuggestItem{Code: "510300", Name: "沪深300ETF", MktNum: "1", SecurityTypeName: "ETF"},
			category: core.HotCategoryCNETF,
			wantOK:   true,
			wantMkt:  "CN-ETF",
		},
		{
			name:     "onshore ETF stays out of CN-A pool",
			item:     eastMoneySuggestItem{Code: "510300", Name: "沪深300ETF", MktNum: "1", SecurityTypeName: "ETF"},
			category: core.HotCategoryCNA,
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seed, ok := eastMoneySuggestToSeed(tt.item, tt.category)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (seed=%+v)", ok, tt.wantOK, seed)
			}
			if tt.wantOK && seed.Market != tt.wantMkt {
				t.Fatalf("market = %q, want %q", seed.Market, tt.wantMkt)
			}
		})
	}
}
