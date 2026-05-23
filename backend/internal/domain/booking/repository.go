package booking

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the Booking aggregate.
type Repository interface {
	Create(ctx context.Context, b *Booking) error
	Update(ctx context.Context, b *Booking) error
	FindByID(ctx context.Context, id uuid.UUID) (*Booking, error)
	ListByGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*Booking], error)
	ListByProperty(ctx context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*Booking], error)
	// HasOverlap reports whether an active booking already occupies the given
	// date range for the property. Used to enforce no double-booking.
	HasOverlap(ctx context.Context, propertyID uuid.UUID, dates DateRange) (bool, error)
}
