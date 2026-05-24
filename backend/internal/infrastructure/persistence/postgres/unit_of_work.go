package postgres

import (
	"context"
	"fmt"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UnitOfWork runs a function inside a single pgx transaction, exposing
// transaction-bound repositories and the outbox so a domain write and the
// events it produces commit atomically. After a successful commit it drains the
// relay so the freshly-recorded events are dispatched to in-process subscribers.
type UnitOfWork struct {
	pool  *pgxpool.Pool
	relay *event.DurablePublisher
}

// NewUnitOfWork builds a UnitOfWork. relay may be nil to skip post-commit
// dispatch (events still persist and are picked up by the periodic relay).
func NewUnitOfWork(pool *pgxpool.Pool, relay *event.DurablePublisher) *UnitOfWork {
	return &UnitOfWork{pool: pool, relay: relay}
}

// Run executes fn inside a transaction, committing on success and rolling back
// on error or panic.
func (u *UnitOfWork) Run(ctx context.Context, fn func(tx port.Tx) error) error {
	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	outbox := event.NewRecordingOutbox(NewOutboxRepository(tx))
	repos := port.Tx{
		Bookings: NewBookingRepository(tx),
		Messages: NewMessageRepository(tx),
		Identity: NewIdentityRepository(tx),
		Outbox:   outbox,
	}
	if err := fn(repos); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	committed = true

	// The write and its events are now durably committed; dispatch exactly the
	// events this transaction appended (not a global drain), so concurrent
	// transactions never re-deliver each other's events. Anything not marked
	// processed here is retried by the periodic recovery relay.
	if u.relay != nil {
		u.relay.DispatchRecords(ctx, outbox.Recorded())
	}
	return nil
}

var _ port.UnitOfWork = (*UnitOfWork)(nil)
