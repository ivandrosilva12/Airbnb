package review

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the Review aggregate.
type Repository interface {
	Create(ctx context.Context, r *Review) error
	ExistsForBooking(ctx context.Context, bookingID uuid.UUID) (bool, error)
	ListByProperty(ctx context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*Review], error)
	SummaryForProperty(ctx context.Context, propertyID uuid.UUID) (Summary, error)
}
