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
		wantSym  string
	}{
		{
			name:     "HK stock stays out of HK ETF pool",
			item:     eastMoneySuggestItem{Code: "00700", Name: "腾讯控股", MktNum: "128", SecurityTypeName: "港股"},
			category: core.HotCategoryHKETF,
			wantOK:   false,
		},
		{
			name:     "HK ETF labelled 港股ETF stays in HK ETF pool",
			item:     eastMoneySuggestItem{Code: "02800", Name: "盈富基金", MktNum: "128", SecurityTypeName: "港股ETF"},
			category: core.HotCategoryHKETF,
			wantOK:   true,
			wantMkt:  "HK-ETF",
			wantSym:  "02800.HK",
		},
		{
			name: "live HK ETF is typed 港股 not 港股ETF",
			item: eastMoneySuggestItem{
				Code: "02800", Name: "盈富基金", MktNum: "116", Classify: "HK", SecurityTypeName: "港股",
			},
			category: core.HotCategoryHKETF,
			wantOK:   true,
			wantMkt:  "HK-ETF",
			wantSym:  "02800.HK",
		},
		{
			name: "HK index tracker without 基金 in the name",
			item: eastMoneySuggestItem{
				Code: "03115", Name: "安硕恒生指数", MktNum: "116", Classify: "HK", SecurityTypeName: "港股",
			},
			category: core.HotCategoryHKETF,
			wantOK:   true,
			wantMkt:  "HK-ETF",
			wantSym:  "03115.HK",
		},
		{
			name: "HK leveraged ETF uses 做空 in the name",
			item: eastMoneySuggestItem{
				Code: "07552", Name: "南方两倍做空恒生科技", MktNum: "116", Classify: "HK", SecurityTypeName: "港股",
			},
			category: core.HotCategoryHKETF,
			wantOK:   true,
			wantMkt:  "HK-ETF",
			wantSym:  "07552.HK",
		},
		{
			name:     "HK ETF stays out of HK stock pool",
			item:     eastMoneySuggestItem{Code: "02800", Name: "盈富基金", MktNum: "128", SecurityTypeName: "港股"},
			category: core.HotCategoryHK,
			wantOK:   false,
		},
		{
			name: "A-share stays in CN-A pool",
			item: eastMoneySuggestItem{
				Code:             "600519",
				Name:             "贵州茅台",
				MktNum:           "1",
				Classify:         "AStock",
				SecurityTypeName: "沪A",
			},
			category: core.HotCategoryCNA,
			wantOK:   true,
			wantMkt:  "CN-A",
			wantSym:  "600519.SH",
		},
		{
			name: "ChiNext 300xxx is still typed 深A",
			item: eastMoneySuggestItem{
				Code:             "300750",
				Name:             "宁德时代",
				MktNum:           "0",
				Classify:         "AStock",
				SecurityTypeName: "深A",
			},
			category: core.HotCategoryCNA,
			wantOK:   true,
			wantMkt:  "CN-GEM",
			wantSym:  "300750.SZ",
		},
		{
			name: "STAR 科创板 is Classify 23 not AStock",
			item: eastMoneySuggestItem{
				Code:             "688981",
				Name:             "中芯国际",
				MktNum:           "1",
				Classify:         "23",
				SecurityTypeName: "科创板",
			},
			category: core.HotCategoryCNA,
			wantOK:   true,
			wantMkt:  "CN-STAR",
			wantSym:  "688981.SH",
		},
		{
			name: "Shanghai index must not become 000001.SH",
			item: eastMoneySuggestItem{
				Code:             "000001",
				Name:             "上证指数",
				MktNum:           "1",
				Classify:         "Index",
				SecurityTypeName: "指数",
			},
			category: core.HotCategoryCNA,
			wantOK:   false,
		},
		{
			name: "Beijing 京A shares MktNum 0 with Shenzhen and is dropped",
			item: eastMoneySuggestItem{
				Code:             "920047",
				Name:             "诺思兰德",
				MktNum:           "0",
				Classify:         "NEEQ",
				SecurityTypeName: "京A",
			},
			category: core.HotCategoryCNA,
			wantOK:   false,
		},
		{
			name: "old NEEQ 三板 is dropped",
			item: eastMoneySuggestItem{
				Code:             "833700",
				Name:             "阿斯克",
				MktNum:           "0",
				Classify:         "NEEQ",
				SecurityTypeName: "三板",
			},
			category: core.HotCategoryCNA,
			wantOK:   false,
		},
		{
			name:     "onshore ETF typed ETF stays in CN-ETF pool",
			item:     eastMoneySuggestItem{Code: "510300", Name: "沪深300ETF", MktNum: "1", SecurityTypeName: "ETF"},
			category: core.HotCategoryCNETF,
			wantOK:   true,
			wantMkt:  "CN-ETF",
			wantSym:  "510300.SH",
		},
		{
			name: "live onshore ETF is typed 基金 Classify Fund",
			item: eastMoneySuggestItem{
				Code: "510300", Name: "沪深300ETF华泰柏瑞", MktNum: "1", Classify: "Fund", SecurityTypeName: "基金",
			},
			category: core.HotCategoryCNETF,
			wantOK:   true,
			wantMkt:  "CN-ETF",
			wantSym:  "510300.SH",
		},
		{
			name: "Shenzhen ChiNext ETF is typed 基金",
			item: eastMoneySuggestItem{
				Code: "159915", Name: "创业板ETF易方达", MktNum: "0", Classify: "Fund", SecurityTypeName: "基金",
			},
			category: core.HotCategoryCNETF,
			wantOK:   true,
			wantMkt:  "CN-ETF",
			wantSym:  "159915.SZ",
		},
		{
			name: "onshore LOF is typed 基金 and belongs in CN-ETF",
			item: eastMoneySuggestItem{
				Code: "161725", Name: "白酒基金LOF", MktNum: "0", Classify: "Fund", SecurityTypeName: "基金",
			},
			category: core.HotCategoryCNETF,
			wantOK:   true,
			wantMkt:  "CN-ETF",
			wantSym:  "161725.SZ",
		},
		{
			name: "onshore ETF stays out of CN-A pool",
			item: eastMoneySuggestItem{
				Code:             "510300",
				Name:             "沪深300ETF华泰柏瑞",
				MktNum:           "1",
				Classify:         "Fund",
				SecurityTypeName: "基金",
			},
			category: core.HotCategoryCNA,
			wantOK:   false,
		},
		{
			name: "HK stock with market 116 stays in HK pool",
			item: eastMoneySuggestItem{
				Code:             "02513",
				Name:             "智谱",
				MktNum:           "116",
				Classify:         "HK",
				SecurityTypeName: "港股",
			},
			category: core.HotCategoryHK,
			wantOK:   true,
			wantMkt:  "HK-MAIN",
			wantSym:  "02513.HK",
		},
		{
			name:     "short HK code is padded",
			item:     eastMoneySuggestItem{Code: "2513", Name: "智谱", MktNum: "116", SecurityTypeName: "港股"},
			category: core.HotCategoryHK,
			wantOK:   true,
			wantMkt:  "HK-MAIN",
			wantSym:  "02513.HK",
		},
		{
			name: "HK GEM 08xxx is not treated as an ETF",
			item: eastMoneySuggestItem{
				Code:             "08083",
				Name:             "有赞",
				MktNum:           "116",
				Classify:         "HK",
				SecurityTypeName: "港股",
			},
			category: core.HotCategoryHK,
			wantOK:   true,
			wantMkt:  "HK-GEM",
			wantSym:  "08083.HK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seed, ok := eastMoneySuggestToSeed(tt.item, tt.category)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (seed=%+v)", ok, tt.wantOK, seed)
			}
			if !tt.wantOK {
				return
			}
			if seed.Market != tt.wantMkt {
				t.Fatalf("market = %q, want %q", seed.Market, tt.wantMkt)
			}
			if tt.wantSym != "" && seed.Symbol != tt.wantSym {
				t.Fatalf("symbol = %q, want %q", seed.Symbol, tt.wantSym)
			}
		})
	}
}

func TestNormaliseEastMoneySuggestKeyword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"2513.HK", "2513"},
		{"2513.hk", "2513"},
		{"02513", "02513"},
		{"600519.SH", "600519"},
		{"000001.SZ", "000001"},
		{"510300.SH", "510300"},
		{"920047.BJ", "920047"},
		{"腾讯", "腾讯"},
	}
	for _, tt := range tests {
		if got := normaliseEastMoneySuggestKeyword(tt.in); got != tt.want {
			t.Fatalf("normaliseEastMoneySuggestKeyword(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPrepareEastMoneySuggestKeyword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in       string
		category core.HotCategory
		want     string
	}{
		{"700.HK", core.HotCategoryHK, "00700"},
		{"700", core.HotCategoryHK, "00700"},
		{"2513.HK", core.HotCategoryHKETF, "02513"},
		{"02800.HK", core.HotCategoryHKETF, "02800"},
		{"600519.SH", core.HotCategoryCNA, "600519"},
		{"510300.SH", core.HotCategoryCNETF, "510300"},
		{"腾讯", core.HotCategoryHK, "腾讯"},
	}
	for _, tt := range tests {
		if got := prepareEastMoneySuggestKeyword(tt.in, tt.category); got != tt.want {
			t.Fatalf("prepareEastMoneySuggestKeyword(%q, %s) = %q, want %q", tt.in, tt.category, got, tt.want)
		}
	}
}

func TestFilterHotSeedsClassShare(t *testing.T) {
	t.Parallel()

	seeds := []hotSeed{{Symbol: "BRK.B", Name: "Berkshire Hathaway", Market: "US-STOCK", Currency: "USD"}}
	if got := filterHotSeeds(seeds, "BRK-B"); len(got) != 1 {
		t.Fatalf("BRK-B should match local pool symbol BRK.B, got %#v", got)
	}
	if got := filterHotSeeds(seeds, "brk.b"); len(got) != 1 {
		t.Fatalf("brk.b should match local pool symbol BRK.B, got %#v", got)
	}
}
