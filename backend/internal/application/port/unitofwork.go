package port

import (
	"context"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/dispute"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/splitpayment"
)

// Tx exposes the repositories that participate in a single atomic unit of work,
// plus the outbox. Writes made through these and the event(s) appended to the
// Outbox commit together, so a domain change and its published events are never
// out of step (no event is lost between the write committing and being
// recorded).
type Tx struct {
	Bookings      booking.Repository
	Messages      message.Repository
	Identity      identity.Repository
	SplitPayments splitpayment.Repository
	// ExperienceBookings, when wired by the UnitOfWork, lets the
	// ExperienceBooking service route Create/Confirm/Cancel/Complete writes
	// through the same atomic-commit-with-outbox path the property-booking
	// service uses (S87 — WF-GAP-020).
	ExperienceBookings experiencebooking.Repository
	// Disputes, when wired, lets the dispute service write Open / admin
	// decisions atomically with the outbox (S89 — WF-GAP-003/013).
	Disputes dispute.Repository
	Outbox   event.OutboxStore
}

// UnitOfWork runs a function inside a transaction. On success the writes and the
// appended outbox events are committed atomically and the events are then
// dispatched; on error everything is rolled back.
type UnitOfWork interface {
	Run(ctx context.Context, fn func(tx Tx) error) error
}
