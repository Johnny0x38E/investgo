package hot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"investgo/internal/core/instrument"
	"investgo/internal/core/pool"
)

// Repositories groups the catalog and pool persistence required by baseline
// seeding. The two repositories may share one SQLite database connection pool.
type Repositories struct {
	Instruments instrument.Repository
	Pools       pool.Repository
}

// SeedBuiltInPools synchronizes the shipped baseline while preserving the
// independent user-override layer.
func SeedBuiltInPools(ctx context.Context, repositories Repositories, dataVersion string) error {
	return seedBuiltInPools(ctx, repositories, dataVersion, BuiltInPoolBaselines())
}

func seedBuiltInPools(ctx context.Context, repositories Repositories, dataVersion string, baselines []BuiltInPoolBaseline) error {
	if repositories.Instruments == nil {
		return errors.New("seed built-in pools: instrument repository is required")
	}
	if repositories.Pools == nil {
		return errors.New("seed built-in pools: pool repository is required")
	}
	dataVersion = strings.TrimSpace(dataVersion)
	if dataVersion == "" {
		return errors.New("seed built-in pools: data version is required")
	}

	for _, baseline := range baselines {
		if err := ctx.Err(); err != nil {
			return err
		}
		definition := baseline.Pool
		definition.DataVersion = dataVersion
		storedPool, err := repositories.Pools.Upsert(ctx, definition)
		if err != nil {
			return fmt.Errorf("seed pool %s: %w", definition.ID, err)
		}

		instrumentIDs := make([]string, 0, len(baseline.Members))
		for _, member := range baseline.Members {
			candidate := instrument.Instrument{
				AssetClass:    storedPool.AssetClass,
				Symbol:        member.Symbol,
				Name:          member.Name,
				Market:        member.Market,
				Exchange:      member.Exchange,
				QuoteCurrency: member.Currency,
			}
			normalized, err := instrument.Normalize(candidate)
			if err != nil {
				return fmt.Errorf("seed instrument %s for pool %s: %w", member.Symbol, definition.ID, err)
			}
			existing, found, err := repositories.Instruments.Find(ctx, normalized.Identity())
			if err != nil {
				return fmt.Errorf("lookup instrument %s for pool %s: %w", member.Symbol, definition.ID, err)
			}
			if found {
				instrumentIDs = append(instrumentIDs, existing.ID)
				continue
			}
			storedInstrument, err := repositories.Instruments.Upsert(ctx, candidate)
			if err != nil {
				return fmt.Errorf("seed instrument %s for pool %s: %w", member.Symbol, definition.ID, err)
			}
			instrumentIDs = append(instrumentIDs, storedInstrument.ID)
		}

		if storedPool.Type != pool.TypeCustom {
			if err := repositories.Pools.ReplaceBuiltInMembers(ctx, storedPool.ID, dataVersion, instrumentIDs); err != nil {
				return fmt.Errorf("seed membership for pool %s: %w", storedPool.ID, err)
			}
		}
	}
	return nil
}
