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
type eastMoneySuggestItem struct {
	Code             string `json:"Code"`
	Name             string `json:"Name"`
	MktNum           string `json:"MktNum"`
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
	params.Set("input", strings.TrimSpace(keyword))
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
	items, err := fetchEastMoneySuggest(ctx, s.client, keyword, 30)
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

// eastMoneySuggestToSeed converts an EastMoney suggest item to a hotSeed,
// returning false if the item does not belong to the given category.
func eastMoneySuggestToSeed(item eastMoneySuggestItem, category core.HotCategory) (hotSeed, bool) {
	code := strings.TrimSpace(item.Code)
	name := strings.TrimSpace(item.Name)
	if code == "" {
		return hotSeed{}, false
	}

	switch item.MktNum {
	case "1": // Shanghai
		if isCNHotCategory(category) && eastMoneySuggestMatchesCategory(item.SecurityTypeName, category) {
			market := "CN-A"
			if category == core.HotCategoryCNETF {
				market = "CN-ETF"
			}
			return hotSeed{Symbol: strings.ToUpper(code) + ".SH", Name: name, Market: market, Currency: "CNY"}, true
		}
	case "0": // Shenzhen
		if isCNHotCategory(category) && eastMoneySuggestMatchesCategory(item.SecurityTypeName, category) {
			market := "CN-A"
			if category == core.HotCategoryCNETF {
				market = "CN-ETF"
			}
			return hotSeed{Symbol: strings.ToUpper(code) + ".SZ", Name: name, Market: market, Currency: "CNY"}, true
		}
	case "128": // Hong Kong
		if isHKHotCategory(category) && eastMoneySuggestMatchesCategory(item.SecurityTypeName, category) {
			market := "HK-MAIN"
			if category == core.HotCategoryHKETF {
				market = "HK-ETF"
			}
			return hotSeed{Symbol: strings.ToUpper(code) + ".HK", Name: name, Market: market, Currency: "HKD"}, true
		}
	}
	return hotSeed{}, false
}

func eastMoneySuggestMatchesCategory(typeName string, category core.HotCategory) bool {
	isETF := strings.Contains(strings.ToUpper(strings.TrimSpace(typeName)), "ETF")
	switch category {
	case core.HotCategoryCNETF, core.HotCategoryHKETF:
		return isETF
	case core.HotCategoryCNA, core.HotCategoryHK:
		return !isETF
	default:
		return true
	}
}
