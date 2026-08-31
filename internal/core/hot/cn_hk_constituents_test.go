package hot

import (
	"testing"

	"investgo/internal/core/instrument"
)

func TestCNHKConstituentsNormalize(t *testing.T) {
	t.Parallel()

	groups := []struct {
		name       string
		seeds      []hotSeed
		assetClass instrument.AssetClass
	}{
		{name: "A-shares", seeds: cnAConstituents, assetClass: instrument.AssetClassEquity},
		{name: "China ETF", seeds: cnETFConstituents, assetClass: instrument.AssetClassETF},
		{name: "Hong Kong", seeds: hkStockConstituents, assetClass: instrument.AssetClassEquity},
		{name: "Hong Kong ETF", seeds: hkETFConstituents, assetClass: instrument.AssetClassETF},
	}
	for _, group := range groups {
		if len(group.seeds) == 0 {
			t.Fatalf("%s constituent list is empty", group.name)
		}
		for _, seed := range group.seeds {
			if _, err := instrument.Normalize(instrument.Instrument{
				AssetClass:    group.assetClass,
				Symbol:        seed.Symbol,
				Name:          seed.Name,
				Market:        seed.Market,
				QuoteCurrency: seed.Currency,
			}); err != nil {
				t.Fatalf("%s seed %s/%s: %v", group.name, seed.Market, seed.Symbol, err)
			}
		}
	}
}
