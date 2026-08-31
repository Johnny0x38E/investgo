package pool

import "context"

// Repository persists pool definitions, versioned built-in membership, and
// user overrides as independent layers.
type Repository interface {
	Get(ctx context.Context, id string) (Pool, bool, error)
	List(ctx context.Context) ([]Pool, error)
	Upsert(ctx context.Context, value Pool) (Pool, error)
	ReplaceBuiltInMembers(ctx context.Context, poolID, dataVersion string, instrumentIDs []string) error
	ListBuiltInMemberIDs(ctx context.Context, poolID string) ([]string, error)
	GetOverride(ctx context.Context, poolID, instrumentID string) (Override, bool, error)
	ListOverrides(ctx context.Context, poolID string) ([]Override, error)
	UpsertOverride(ctx context.Context, value Override) (Override, error)
	DeleteOverride(ctx context.Context, poolID, instrumentID string) error
	GetEdit(ctx context.Context, poolID, instrumentID string) (Edit, bool, error)
	UpsertEdit(ctx context.Context, value Edit) (Edit, error)
	DeleteEdit(ctx context.Context, poolID, instrumentID string) error
}
