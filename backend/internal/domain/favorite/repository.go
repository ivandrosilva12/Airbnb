package favorite

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the favorite context.
type Repository interface {
	// Add saves a favorite. Adding an existing favorite is a no-op (idempotent).
	Add(ctx context.Context, f *Favorite) error
	// Remove deletes a favorite; removing a missing one is a no-op.
	Remove(ctx context.Context, userID, propertyID uuid.UUID) error
	Exists(ctx context.Context, userID, propertyID uuid.UUID) (bool, error)
	// ListPropertyIDs returns the property IDs a user has favorited, newest first.
	ListPropertyIDs(ctx context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[uuid.UUID], error)
}
