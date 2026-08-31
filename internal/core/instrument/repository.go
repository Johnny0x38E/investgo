package instrument

import "context"

// Repository persists canonical instruments independently from provider symbol
// mappings and from the user features that reference instruments.
type Repository interface {
	Get(ctx context.Context, id string) (Instrument, bool, error)
	Find(ctx context.Context, identity Identity) (Instrument, bool, error)
	Upsert(ctx context.Context, value Instrument) (Instrument, error)
	GetProviderSymbol(ctx context.Context, instrumentID, providerID string) (ProviderSymbol, bool, error)
	UpsertProviderSymbol(ctx context.Context, value ProviderSymbol) (ProviderSymbol, error)
	DeleteProviderSymbol(ctx context.Context, instrumentID, providerID string) error
}
