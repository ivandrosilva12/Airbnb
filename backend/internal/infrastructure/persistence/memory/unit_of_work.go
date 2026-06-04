package memory

import (
	"context"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/dispute"
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
	disputes           dispute.Repository
	experienceBookings experiencebooking.Repository
	outbox             event.OutboxStore
	relay              *event.DurablePublisher
	// commitErr, when non-nil, is returned after fn ran successfully — as if
	// the underlying DB rejected the commit. It lets unit tests simulate a
	// commit-time crash so they can prove the side-effects (writes + outbox
	// dispatch) were rolled back together with the recorded events.
	commitErr error
}

// NewUnitOfWork builds an in-memory UnitOfWork. relay may be nil to skip
// dispatch (events are still recorded in the outbox). Any repository may be nil
// when the test doesn't exercise that aggregate's path; the corresponding Tx
// field will be nil and any caller touching it will panic clearly.
func NewUnitOfWork(bookings booking.Repository, messages message.Repository, identity identity.Repository, splitPayments splitpayment.Repository, disputes dispute.Repository, outbox event.OutboxStore, relay *event.DurablePublisher) *UnitOfWork {
	return &UnitOfWork{bookings: bookings, messages: messages, identity: identity, splitPayments: splitPayments, disputes: disputes, outbox: outbox, relay: relay}
}

// WithCommitError installs a synthetic commit failure: Run executes fn, then
// returns err without dispatching the recorded events. Tests use it to assert
// that a commit-time failure rolls everything back together — no event leaks
// to subscribers and no caller sees a partial success.
func (u *UnitOfWork) WithCommitError(err error) *UnitOfWork {
	u.commitErr = err
	return u
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
// events. A failure in fn is returned without dispatching. When the unit was
// configured with WithCommitError, fn's outputs are discarded and the configured
// error is returned (simulating a commit-time rollback) — the recorded events
// are not dispatched.
func (u *UnitOfWork) Run(ctx context.Context, fn func(tx port.Tx) error) error {
	outbox := event.NewRecordingOutbox(u.outbox)
	if err := fn(port.Tx{
		Bookings:           u.bookings,
		Messages:           u.messages,
		Identity:           u.identity,
		SplitPayments:      u.splitPayments,
		Disputes:           u.disputes,
		ExperienceBookings: u.experienceBookings,
		Outbox:             outbox,
	}); err != nil {
		return err
	}
	if u.commitErr != nil {
		// The simulated rollback: skip dispatching the events the unit
		// recorded, mirroring what production would do if the DB commit
		// failed (the writes never reached storage, so no event should
		// fan out).
		return u.commitErr
	}
	// Dispatch exactly the events appended in this unit of work (matching the
	// Postgres path), so concurrent units never re-deliver each other's events.
	if u.relay != nil {
		u.relay.DispatchRecords(ctx, outbox.Recorded())
	}
	return nil
}

var _ port.UnitOfWork = (*UnitOfWork)(nil)
