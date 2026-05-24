package identity

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the Verification aggregate.
type Repository interface {
	Create(ctx context.Context, v *Verification) error
	Update(ctx context.Context, v *Verification) error
	FindByID(ctx context.Context, id uuid.UUID) (*Verification, error)
	// FindLatestByUser returns the user's most recent verification, or
	// shared.ErrNotFound if they have never submitted one.
	FindLatestByUser(ctx context.Context, userID uuid.UUID) (*Verification, error)
	// ListByStatus returns verifications in the given status, newest first —
	// used to drive the administrator review queue.
	ListByStatus(ctx context.Context, status Status, page shared.Page) (shared.PageResult[*Verification], error)
}
