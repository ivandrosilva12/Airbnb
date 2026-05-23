package booking

import (
	"context"
	"time"

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
	// ListActiveInRange returns active (pending/confirmed) bookings that overlap
	// the [from, to) window for a property. Used to build the availability view;
	// the window may include past dates, so raw times are taken rather than a
	// validated DateRange.
	ListActiveInRange(ctx context.Context, propertyID uuid.UUID, from, to time.Time) ([]*Booking, error)
	// BookedPropertyIDs returns the distinct property IDs that have an active
	// booking overlapping the [from, to) window. Used by date-aware search to
	// exclude unavailable listings.
	BookedPropertyIDs(ctx context.Context, from, to time.Time) ([]uuid.UUID, error)
}
