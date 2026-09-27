package hot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"investgo/internal/core"
	"investgo/internal/core/endpoint"
	"investgo/internal/core/provider"
)

// eastMoneySuggestResponse models the JSON envelope returned by the EastMoney suggest API.
type eastMoneySuggestResponse struct {
	QuotationCodeTable struct {
		Data []eastMoneySuggestItem `json:"Data"`
	} `json:"QuotationCodeTable"`
}

// eastMoneySuggestItem represents a single result from the EastMoney suggest API.
//
// Live suggest labels do not match the board names InvestGo uses. The fields
// below are the ones we actually key off; keep them in sync with
// classifyEastMoneySuggest.
type eastMoneySuggestItem struct {
	Code             string `json:"Code"`
	Name             string `json:"Name"`
	MktNum           string `json:"MktNum"`
	Classify         string `json:"Classify"`
	SecurityTypeName string `json:"SecurityTypeName"`
}

// eastMoneyHotResponse models the JSON envelope returned by the EastMoney clist API.
type eastMoneyHotResponse struct {
	RC   int `json:"rc"`
	Data struct {
		Total int                  `json:"total"`
		Diff  eastMoneyHotDiffList `json:"diff"`
	} `json:"data"`
}

type eastMoneyHotDiffList []eastMoneyHotDiff

func (l *eastMoneyHotDiffList) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*l = nil
		return nil
	}

	var asArray []eastMoneyHotDiff
	if err := json.Unmarshal(data, &asArray); err == nil {
		*l = asArray
		return nil
	}

	var asMap map[string]eastMoneyHotDiff
	if err := json.Unmarshal(data, &asMap); err != nil {
		return err
	}

	out := make([]eastMoneyHotDiff, 0, len(asMap))
	for _, item := range asMap {
		out = append(out, item)
	}
	*l = out
	return nil
}

type eastMoneyHotDiff struct {
	MarketID      int              `json:"f13"`
	Code          string           `json:"f12"`
	Name          string           `json:"f14"`
	CurrentPrice  provider.EmFloat `json:"f2"`
	ChangePercent provider.EmFloat `json:"f3"`
	Change        provider.EmFloat `json:"f4"`
	Volume        provider.EmFloat `json:"f5"`
	MarketCap     provider.EmFloat `json:"f20"`
}

// normaliseEastMoneyCode pads leading zeros for the EastMoney returned code based on marketID.
func normaliseEastMoneyCode(code string, marketID int) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	switch marketID {
	case 116, 128:
		if len(code) < 5 && core.IsDigits(code) {
			return strings.Repeat("0", 5-len(code)) + code
		}

	case 0, 1:
		if len(code) < 6 && core.IsDigits(code) {
			return strings.Repeat("0", 6-len(code)) + code
		}
	}
	return code
}

// fetchEastMoneySuggest calls the EastMoney suggest API to search stocks by keyword (name or code).
// Returns up to the requested number of matching items across all markets.
// Callers that know the pool category should run prepareEastMoneySuggestKeyword
// first so HK short codes are padded and Yahoo-style suffixes are stripped.
func fetchEastMoneySuggest(
	ctx context.Context,
	client *http.Client,
	keyword string,
	count int,
) ([]eastMoneySuggestItem, error) {
	if client == nil {
		client = &http.Client{}
	}
	if count <= 0 {
		count = 30
	}

	params := url.Values{}
	params.Set("input", normaliseEastMoneySuggestKeyword(keyword))
	params.Set("type", "14")
	params.Set("token", "D43BF722C8E33BDC906FB84D85E326E8")
	params.Set("count", strconv.Itoa(count))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.EastMoneySuggestAPI+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	provider.SetEastMoneyHeaders(req, endpoint.EastMoneyWebReferer)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() // nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("EastMoney suggest request failed: status %d", resp.StatusCode)
	}

	var parsed eastMoneySuggestResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return parsed.QuotationCodeTable.Data, nil
}

// searchEastMoneySeeds calls the EastMoney suggest API and returns matching seeds for the given category.
func (s *HotService) searchEastMoneySeeds(ctx context.Context, keyword string, category core.HotCategory) []hotSeed {
	items, err := fetchEastMoneySuggest(ctx, s.client, prepareEastMoneySuggestKeyword(keyword, category), 30)
	if err != nil {
		s.log.Warn("EastMoney suggest failed", "keyword", keyword, "error", err)
		return nil
	}

	seeds := make([]hotSeed, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		seed, ok := eastMoneySuggestToSeed(item, category)
		if !ok {
			continue
		}
		key := seed.Market + "|" + seed.Symbol
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		seeds = append(seeds, seed)
	}
	return seeds
}

// eastMoneySuggestKind is the InvestGo-side classification of one EastMoney
// suggest row. EastMoney's own SecurityTypeName / Classify / MktNum values do
// not line up with our pool categories; this enum is the translated result.
type eastMoneySuggestKind int

const (
	eastMoneySuggestUnknown eastMoneySuggestKind = iota
	eastMoneySuggestCNStock
	eastMoneySuggestCNFund
	eastMoneySuggestHKStock
	eastMoneySuggestHKFund
)

// eastMoneySuggestToSeed converts an EastMoney suggest item to a hotSeed,
// returning false if the item does not belong to the given category.
func eastMoneySuggestToSeed(item eastMoneySuggestItem, category core.HotCategory) (hotSeed, bool) {
	code := strings.TrimSpace(item.Code)
	name := strings.TrimSpace(item.Name)
	if code == "" {
		return hotSeed{}, false
	}

	kind := classifyEastMoneySuggest(item)
	if !eastMoneySuggestMatchesCategory(kind, category) {
		return hotSeed{}, false
	}

	switch kind {
	case eastMoneySuggestCNStock:
		exchange, market := eastMoneyCNStockVenue(item)
		return hotSeed{Symbol: strings.ToUpper(code) + "." + exchange, Name: name, Market: market, Currency: "CNY"}, true
	case eastMoneySuggestCNFund:
		return hotSeed{Symbol: strings.ToUpper(code) + "." + eastMoneyCNExchange(item.MktNum), Name: name, Market: "CN-ETF", Currency: "CNY"}, true
	case eastMoneySuggestHKStock:
		return hotSeed{Symbol: padHKSuggestCode(code) + ".HK", Name: name, Market: eastMoneyHKStockMarket(code), Currency: "HKD"}, true
	case eastMoneySuggestHKFund:
		return hotSeed{Symbol: padHKSuggestCode(code) + ".HK", Name: name, Market: "HK-ETF", Currency: "HKD"}, true
	default:
		return hotSeed{}, false
	}
}

// classifyEastMoneySuggest maps a live EastMoney suggest row onto an InvestGo
// kind. The labels below were probed against the public suggest API and MUST
// stay documented: a naive "type contains ETF" / "MktNum 0 = Shenzhen" filter
// drops listed names.
//
// CN boards (MktNum 0 Shenzhen / 1 Shanghai — both also used by other boards):
//   - 沪A / 深A / A股 / Classify AStock → ordinary A-share
//   - 科创板 (Classify is often "23", not AStock) → still a CN equity, STAR
//   - 创业板 → ChiNext; many ChiNext rows are still labelled 深A (e.g. 300750)
//   - 基金 + Classify Fund → listed ETF/LOF/REIT. Type is "基金", not "ETF"
//   - 指数 / Classify Index → skip (000001 上证指数 would otherwise become 000001.SH)
//   - 京A / Classify NEEQ 92xxxx → Beijing. MktNum is 0, same as Shenzhen, so a
//     suffix of .SZ is wrong; EastMoney realtime also does not quote CN-BJ
//   - 三板 / Classify NEEQ 83xxxx → old NEEQ, not a listed A-share
//   - 期权 → skip
//
// HK boards (MktNum 116 and 128 are both used for the same HK names):
//   - 港股 stocks and ETFs share SecurityTypeName "港股". 02800 盈富基金 is not
//     "港股ETF". SecurityType "6" is also shared with GEM stocks (08083 有赞),
//     so it cannot split the HK / HK-ETF pools
//   - listed HK funds are recognised by name markers (ETF / 基金 / LOF / 指数 /
//     做多 / 做空), which covers 02800, 03115 安硕恒生指数, 07552 杠杆/反向
func classifyEastMoneySuggest(item eastMoneySuggestItem) eastMoneySuggestKind {
	switch strings.TrimSpace(item.MktNum) {
	case "0", "1":
		if eastMoneySuggestIsIndexOrOption(item) || eastMoneySuggestIsBeijingOrNEEQ(item) {
			return eastMoneySuggestUnknown
		}
		if eastMoneySuggestIsCNListedFund(item) {
			return eastMoneySuggestCNFund
		}
		if eastMoneySuggestIsCNEquity(item) {
			return eastMoneySuggestCNStock
		}
	case "116", "128":
		if eastMoneySuggestIsHKListedFund(item) {
			return eastMoneySuggestHKFund
		}
		return eastMoneySuggestHKStock
	}
	return eastMoneySuggestUnknown
}

func eastMoneySuggestIsIndexOrOption(item eastMoneySuggestItem) bool {
	typeName := strings.TrimSpace(item.SecurityTypeName)
	classify := strings.TrimSpace(item.Classify)
	return typeName == "指数" || typeName == "期权" || strings.EqualFold(classify, "Index")
}

func eastMoneySuggestIsBeijingOrNEEQ(item eastMoneySuggestItem) bool {
	typeName := strings.TrimSpace(item.SecurityTypeName)
	classify := strings.TrimSpace(item.Classify)
	// 京A (920xxx) and 三板 (83xxxx) both arrive as MktNum 0 / Classify NEEQ.
	// Treating them as Shenzhen would emit 920047.SZ. Drop them here rather
	// than remap to CN-BJ: EastMoney realtime quotes reject CN-BJ.
	return typeName == "京A" || typeName == "三板" || strings.EqualFold(classify, "NEEQ")
}

func eastMoneySuggestIsCNListedFund(item eastMoneySuggestItem) bool {
	typeName := strings.ToUpper(strings.TrimSpace(item.SecurityTypeName))
	classify := strings.TrimSpace(item.Classify)
	name := strings.ToUpper(strings.TrimSpace(item.Name))
	return strings.EqualFold(classify, "Fund") ||
		strings.Contains(typeName, "基金") ||
		strings.Contains(typeName, "ETF") ||
		strings.Contains(typeName, "LOF") ||
		strings.Contains(typeName, "REIT") ||
		strings.Contains(name, "ETF")
}

func eastMoneySuggestIsCNEquity(item eastMoneySuggestItem) bool {
	typeName := strings.TrimSpace(item.SecurityTypeName)
	classify := strings.TrimSpace(item.Classify)
	switch typeName {
	case "沪A", "深A", "A股", "科创板", "创业板":
		return true
	}
	return strings.EqualFold(classify, "AStock")
}

func eastMoneySuggestIsHKListedFund(item eastMoneySuggestItem) bool {
	typeName := strings.ToUpper(strings.TrimSpace(item.SecurityTypeName))
	if strings.Contains(typeName, "ETF") {
		return true
	}
	name := strings.TrimSpace(item.Name)
	upperName := strings.ToUpper(name)
	// Name markers: type stays "港股" for both 00700 腾讯 and 02800 盈富基金.
	for _, marker := range []string{"ETF", "基金", "LOF", "指数", "做多", "做空"} {
		if strings.Contains(name, marker) || strings.Contains(upperName, strings.ToUpper(marker)) {
			return true
		}
	}
	return false
}

func eastMoneyCNExchange(mktNum string) string {
	if strings.TrimSpace(mktNum) == "1" {
		return "SH"
	}
	return "SZ"
}

func eastMoneyCNStockVenue(item eastMoneySuggestItem) (exchange, market string) {
	exchange = eastMoneyCNExchange(item.MktNum)
	code := strings.TrimSpace(item.Code)
	typeName := strings.TrimSpace(item.SecurityTypeName)
	switch {
	case typeName == "科创板" || strings.HasPrefix(code, "688") || strings.HasPrefix(code, "689"):
		return "SH", "CN-STAR"
	case typeName == "创业板" || (exchange == "SZ" && strings.HasPrefix(code, "3")):
		return "SZ", "CN-GEM"
	default:
		return exchange, "CN-A"
	}
}

func eastMoneyHKStockMarket(code string) string {
	// HK GEM listed names are 08xxx (e.g. 08083 有赞). Main-board and ETF
	// codes also share MktNum 116 and type 港股; GEM is the only numeric range
	// we can safely special-case without colliding with 07xxx leveraged ETFs.
	if strings.HasPrefix(padHKSuggestCode(code), "08") {
		return "HK-GEM"
	}
	return "HK-MAIN"
}

func eastMoneySuggestMatchesCategory(kind eastMoneySuggestKind, category core.HotCategory) bool {
	switch category {
	case core.HotCategoryCNETF:
		return kind == eastMoneySuggestCNFund
	case core.HotCategoryCNA:
		return kind == eastMoneySuggestCNStock
	case core.HotCategoryHKETF:
		return kind == eastMoneySuggestHKFund
	case core.HotCategoryHK:
		return kind == eastMoneySuggestHKStock
	default:
		return false
	}
}

// prepareEastMoneySuggestKeyword rewrites a user query into the form EastMoney
// suggest actually indexes.
//
//   - Yahoo-style suffixes are ignored by suggest: 2513.HK / 600519.SH / 510300.SH
//     all return 0 rows, while the bare code hits.
//   - HK listed codes are 5-digit. "700" / "700.HK" does not return 00700 腾讯
//     (top hits are A-shares whose codes contain 700). Pad digit-only HK
//     queries once the suffix is stripped.
func prepareEastMoneySuggestKeyword(keyword string, category core.HotCategory) string {
	keyword = normaliseEastMoneySuggestKeyword(keyword)
	if isHKHotCategory(category) {
		return padHKSuggestCode(keyword)
	}
	return keyword
}

func normaliseEastMoneySuggestKeyword(keyword string) string {
	keyword = strings.TrimSpace(keyword)
	upper := strings.ToUpper(keyword)
	for _, suffix := range []string{".HK", ".SH", ".SZ", ".BJ"} {
		if stripped, ok := strings.CutSuffix(upper, suffix); ok {
			return stripped
		}
	}
	return keyword
}

func padHKSuggestCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if core.IsDigits(code) && len(code) < 5 {
		return strings.Repeat("0", 5-len(code)) + code
	}
	return code
}
