package experiencebooking

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the ExperienceBooking
// aggregate.
type Repository interface {
	Create(ctx context.Context, b *Booking) error
	Update(ctx context.Context, b *Booking) error
	FindByID(ctx context.Context, id uuid.UUID) (*Booking, error)
	// ListByGuest returns every booking made by the guest, newest first,
	// across all statuses. Powers the guest's "My experiences" list.
	ListByGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*Booking], error)
	// ListByHost returns every booking against any of the host's
	// experiences, newest first. Powers the host dashboard.
	ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*Booking], error)
	// FindOverlapping returns every non-cancelled booking for the same
	// experience whose session window overlaps the supplied window.
	// Callers use this to refuse a second booking on the same start time
	// when the host has not modelled multi-cohort sessions.
	FindOverlapping(ctx context.Context, experienceID uuid.UUID, startAt, endAt time.Time) ([]*Booking, error)
}
