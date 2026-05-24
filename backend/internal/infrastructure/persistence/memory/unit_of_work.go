package memory

import (
	"context"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/message"
)

// UnitOfWork is the in-memory UnitOfWork used by tests and the e2e harness. It
// has no real transaction (in-memory writes are already mutex-guarded), but it
// preserves the same contract: it runs the function against the shared
// repositories and the outbox, then drains the relay so events are dispatched
// synchronously (matching the previous behavior the tests rely on).
type UnitOfWork struct {
	bookings booking.Repository
	messages message.Repository
	identity identity.Repository
	outbox   event.OutboxStore
	relay    *event.DurablePublisher
}

// NewUnitOfWork builds an in-memory UnitOfWork. relay may be nil to skip
// dispatch (events are still recorded in the outbox).
func NewUnitOfWork(bookings booking.Repository, messages message.Repository, identity identity.Repository, outbox event.OutboxStore, relay *event.DurablePublisher) *UnitOfWork {
	return &UnitOfWork{bookings: bookings, messages: messages, identity: identity, outbox: outbox, relay: relay}
}

// Run executes fn against the shared repositories, then dispatches any recorded
// events. A failure in fn is returned without dispatching.
func (u *UnitOfWork) Run(ctx context.Context, fn func(tx port.Tx) error) error {
	if err := fn(port.Tx{
		Bookings: u.bookings,
		Messages: u.messages,
		Identity: u.identity,
		Outbox:   u.outbox,
	}); err != nil {
		return err
	}
	if u.relay != nil {
		_, _ = u.relay.Recover(ctx, 100)
	}
	return nil
}

var _ port.UnitOfWork = (*UnitOfWork)(nil)
