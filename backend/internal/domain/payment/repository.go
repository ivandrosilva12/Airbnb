package payment

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the Payment aggregate.
type Repository interface {
	Create(ctx context.Context, p *Payment) error
	Update(ctx context.Context, p *Payment) error
	FindByID(ctx context.Context, id uuid.UUID) (*Payment, error)
	FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*Payment, error)
	ListByGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*Payment], error)
}
