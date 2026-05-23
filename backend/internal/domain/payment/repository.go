package payment

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Revenue is a read-model aggregating payment amounts for reporting.
type Revenue struct {
	CapturedCents int64
	PendingCents  int64 // authorized but not yet captured
	Currency      string
}

// Repository is the persistence port for the Payment aggregate.
type Repository interface {
	Create(ctx context.Context, p *Payment) error
	Update(ctx context.Context, p *Payment) error
	FindByID(ctx context.Context, id uuid.UUID) (*Payment, error)
	FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*Payment, error)
	ListByGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*Payment], error)
	// RevenueForBookings totals captured and pending amounts across the given
	// bookings. Assumes a single currency (the platform default); the currency
	// observed is returned for display.
	RevenueForBookings(ctx context.Context, bookingIDs []uuid.UUID) (Revenue, error)
}
