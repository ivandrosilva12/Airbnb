package port

import (
	"context"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/message"
)

// Tx exposes the repositories that participate in a single atomic unit of work,
// plus the outbox. Writes made through these and the event(s) appended to the
// Outbox commit together, so a domain change and its published events are never
// out of step (no event is lost between the write committing and being
// recorded).
type Tx struct {
	Bookings booking.Repository
	Messages message.Repository
	Identity identity.Repository
	Outbox   event.OutboxStore
}

// UnitOfWork runs a function inside a transaction. On success the writes and the
// appended outbox events are committed atomically and the events are then
// dispatched; on error everything is rolled back.
type UnitOfWork interface {
	Run(ctx context.Context, fn func(tx Tx) error) error
}
