package memory

import (
	"context"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/splitpayment"
)

// UnitOfWork is the in-memory UnitOfWork used by tests and the e2e harness. It
// has no real transaction (in-memory writes are already mutex-guarded), but it
// preserves the same contract: it runs the function against the shared
// repositories and the outbox, then drains the relay so events are dispatched
// synchronously (matching the previous behavior the tests rely on).
type UnitOfWork struct {
	bookings           booking.Repository
	messages           message.Repository
	identity           identity.Repository
	splitPayments      splitpayment.Repository
	experienceBookings experiencebooking.Repository
	outbox             event.OutboxStore
	relay              *event.DurablePublisher
}

// NewUnitOfWork builds an in-memory UnitOfWork. relay may be nil to skip
// dispatch (events are still recorded in the outbox). splitPayments may be nil
// when the test doesn't exercise the split-payment path; the Tx field will be
// nil and any caller touching it will panic clearly.
func NewUnitOfWork(bookings booking.Repository, messages message.Repository, identity identity.Repository, splitPayments splitpayment.Repository, outbox event.OutboxStore, relay *event.DurablePublisher) *UnitOfWork {
	return &UnitOfWork{bookings: bookings, messages: messages, identity: identity, splitPayments: splitPayments, outbox: outbox, relay: relay}
}

// WithExperienceBookings wires the ExperienceBooking repository into the UoW so
// the experiencebookingapp service can route its writes through the same
// atomic-commit-with-outbox path as the property-booking service (S87). Nil
// leaves the tx.ExperienceBookings field unset — callers that touch it will
// panic clearly, matching the pre-existing splitPayments contract.
func (u *UnitOfWork) WithExperienceBookings(repo experiencebooking.Repository) *UnitOfWork {
	u.experienceBookings = repo
	return u
}

// Run executes fn against the shared repositories, then dispatches any recorded
// events. A failure in fn is returned without dispatching.
func (u *UnitOfWork) Run(ctx context.Context, fn func(tx port.Tx) error) error {
	outbox := event.NewRecordingOutbox(u.outbox)
	if err := fn(port.Tx{
		Bookings:           u.bookings,
		Messages:           u.messages,
		Identity:           u.identity,
		SplitPayments:      u.splitPayments,
		ExperienceBookings: u.experienceBookings,
		Outbox:             outbox,
	}); err != nil {
		return err
	}
	// Dispatch exactly the events appended in this unit of work (matching the
	// Postgres path), so concurrent units never re-deliver each other's events.
	if u.relay != nil {
		u.relay.DispatchRecords(ctx, outbox.Recorded())
	}
	return nil
}

var _ port.UnitOfWork = (*UnitOfWork)(nil)
