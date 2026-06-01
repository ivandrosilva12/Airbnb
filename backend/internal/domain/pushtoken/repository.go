package pushtoken

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for device push tokens. Save is an upsert
// keyed on (platform, token): when the same physical device re-registers it
// updates the row's user (in case the device switched accounts) and last_seen,
// rather than creating a duplicate row.
type Repository interface {
	Save(ctx context.Context, t *Token) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*Token, error)
	DeleteByToken(ctx context.Context, platform Platform, token string) error
	DeleteByUser(ctx context.Context, userID uuid.UUID) error
}
