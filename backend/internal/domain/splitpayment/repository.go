package splitpayment

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the SplitPayment aggregate. Shares
// are persisted as part of the aggregate (Create/Update write both rows
// atomically); implementations should not expose share-level CRUD.
type Repository interface {
	Create(ctx context.Context, sp *SplitPayment) error
	Update(ctx context.Context, sp *SplitPayment) error
	FindByID(ctx context.Context, id uuid.UUID) (*SplitPayment, error)
	// FindByBookingID returns the split attached to a booking, or
	// shared.ErrNotFound if the booking has no split.
	FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*SplitPayment, error)
	// ListForUser returns the splits where the user is either the organizer
	// or a share payer (matched by email). Newest first.
	ListForUser(ctx context.Context, userID uuid.UUID, email string) ([]*SplitPayment, error)
}
