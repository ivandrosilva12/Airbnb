package block

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the Block aggregate.
type Repository interface {
	Create(ctx context.Context, b *Block) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindByID(ctx context.Context, id uuid.UUID) (*Block, error)
	ListByProperty(ctx context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*Block], error)
	// HasOverlap reports whether a block already covers part of the given range.
	HasOverlap(ctx context.Context, propertyID uuid.UUID, dates booking.DateRange) (bool, error)
	// ListInRange returns blocks overlapping the [from, to) window for a property.
	ListInRange(ctx context.Context, propertyID uuid.UUID, from, to time.Time) ([]*Block, error)
	// BlockedPropertyIDs returns property IDs with a block overlapping [from, to).
	BlockedPropertyIDs(ctx context.Context, from, to time.Time) ([]uuid.UUID, error)
}
