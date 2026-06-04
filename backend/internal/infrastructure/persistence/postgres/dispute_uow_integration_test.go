package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/dispute"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
)

// S114 — Postgres integration tests for the dispute Resolve flow inside
// a UnitOfWork (S89 / WF-GAP-003/013). The application-layer / memory tests
// already prove that the dispute service routes Resolve through the UoW and
// appends a DisputeResolved record; this file closes the gap by exercising
// the actual pgx.Tx-bound DisputeRepository so the dispute UPDATE and the
// outbox INSERT really do commit (or roll back) together against a live
// Postgres.
//
// Each test:
//   - skips cleanly when POSTGRES_TEST_DSN is unset (the suite stays green
//     for dev machines without a database)
//   - reuses the withTestDB / truncate / seed helpers from integration_test.go
//   - rolls its own UnitOfWork against the live pool so the closure sees a
//     pgx.Tx-bound DisputeRepository the same way the dispute service does

// TestDisputeUoW_ResolveWritesAtomicallyWithOutbox_Integration — the happy
// path. Inside a UoW we (a) call AdminResolve on the dispute aggregate and
// persist the decided state via tx.Disputes.Save, AND (b) append a
// DisputeResolved record to tx.Outbox. After commit, the dispute row must
// show status="resolved" AND the outbox must hold the event in pending
// state (processed_at, failed_at, dead_lettered_at all NULL).
func TestDisputeUoW_ResolveWritesAtomicallyWithOutbox_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	// Seed a booking (host+property+guest+booking) and an open dispute
	// against it. The booking is created via the UoW so the FK chain is
	// satisfied; the dispute is created directly through the pool-bound
	// repo so it is independently committed before the test UoW runs.
	b, _ := newTestBooking(t, pool)
	if err := pg.NewUnitOfWork(pool, nil).Run(ctx, func(tx port.Tx) error {
		return tx.Bookings.Create(ctx, b)
	}); err != nil {
		t.Fatalf("seed booking: %v", err)
	}
	d, err := dispute.New(b.ID, b.GuestID, dispute.KindRefund, "broken AC; refund requested", 2500, "EUR")
	if err != nil {
		t.Fatalf("new dispute: %v", err)
	}
	if err := pg.NewDisputeRepository(pool).Save(ctx, d); err != nil {
		t.Fatalf("seed dispute: %v", err)
	}

	// Run the UoW: decide the dispute + append the event. This mirrors what
	// dispute.Service.AdminDecide does after S89 — the in-tx path is the
	// whole point of the slice (write + event commit atomically).
	adminID := uuid.New()
	uow := pg.NewUnitOfWork(pool, nil)
	if err := uow.Run(ctx, func(tx port.Tx) error {
		if err := d.AdminResolve(adminID, "partial refund of 25 EUR approved"); err != nil {
			return err
		}
		if err := tx.Disputes.Save(ctx, d); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.DisputeResolved{
			DisputeID:  d.ID,
			BookingID:  b.ID,
			HostID:     uuid.New(), // irrelevant to the row-state assertions
			GuestID:    b.GuestID,
			Outcome:    "resolved",
			Resolution: d.Resolution,
		})
		if err != nil {
			return err
		}
		return tx.Outbox.Append(ctx, rec)
	}); err != nil {
		t.Fatalf("uow run: %v", err)
	}

	// Assertion 1: the dispute row landed and is now "resolved".
	reloaded, err := pg.NewDisputeRepository(pool).FindByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("reload dispute: %v", err)
	}
	if reloaded.Status != dispute.StatusResolved {
		t.Errorf("dispute status after commit = %q, want %q", reloaded.Status, dispute.StatusResolved)
	}
	if reloaded.Resolution == "" {
		t.Errorf("resolution text empty after commit; AdminResolve write did not persist")
	}
	if reloaded.DecidedAt == nil {
		t.Errorf("decided_at NULL after commit; AdminResolve write did not persist")
	}

	// Assertion 2: the outbox has exactly one DisputeResolved row tied to
	// this dispute, still in pending state (none of the terminal column
	// timestamps populated). The check uses the DisputeID extracted from the
	// JSONB payload so it survives other suite tests running in parallel.
	var pending int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'DisputeID' = $2
		   AND processed_at IS NULL
		   AND failed_at IS NULL
		   AND dead_lettered_at IS NULL`,
		(event.DisputeResolved{}).EventName(), d.ID.String(),
	).Scan(&pending); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending outbox rows for dispute %s = %d, want 1 (the UoW commit must have appended exactly one DisputeResolved record)", d.ID, pending)
	}
}

// TestDisputeUoW_RollbackOnError_Integration — same setup, but the UoW
// closure returns an error after both writes. After rollback, the dispute
// must STILL be "open" (the AdminResolve UPDATE rolled back) AND the
// outbox must have NO row carrying this dispute's id (the Append rolled
// back too). This is the contract S89 exists to enforce — without the UoW,
// a partial commit could ship a DisputeResolved event for a dispute that
// the DB still reports as open.
func TestDisputeUoW_RollbackOnError_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	b, _ := newTestBooking(t, pool)
	if err := pg.NewUnitOfWork(pool, nil).Run(ctx, func(tx port.Tx) error {
		return tx.Bookings.Create(ctx, b)
	}); err != nil {
		t.Fatalf("seed booking: %v", err)
	}
	d, err := dispute.New(b.ID, b.GuestID, dispute.KindRefund, "broken AC; refund requested", 2500, "EUR")
	if err != nil {
		t.Fatalf("new dispute: %v", err)
	}
	if err := pg.NewDisputeRepository(pool).Save(ctx, d); err != nil {
		t.Fatalf("seed dispute: %v", err)
	}

	adminID := uuid.New()
	wantErr := errors.New("simulated failure after dispute save")
	uow := pg.NewUnitOfWork(pool, nil)
	err = uow.Run(ctx, func(tx port.Tx) error {
		if err := d.AdminResolve(adminID, "partial refund of 25 EUR approved"); err != nil {
			return err
		}
		if err := tx.Disputes.Save(ctx, d); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.DisputeResolved{
			DisputeID:  d.ID,
			BookingID:  b.ID,
			GuestID:    b.GuestID,
			Outcome:    "resolved",
			Resolution: d.Resolution,
		})
		if err != nil {
			return err
		}
		if err := tx.Outbox.Append(ctx, rec); err != nil {
			return err
		}
		return wantErr // force rollback AFTER both writes
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("uow run err = %v, want %v", err, wantErr)
	}

	// Assertion 1: the dispute on disk is still "open" — the AdminResolve
	// UPDATE rolled back with the transaction.
	reloaded, err := pg.NewDisputeRepository(pool).FindByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("reload dispute: %v", err)
	}
	if reloaded.Status != dispute.StatusOpen {
		t.Errorf("dispute status after rollback = %q, want %q", reloaded.Status, dispute.StatusOpen)
	}
	if reloaded.Resolution != "" {
		t.Errorf("resolution text = %q after rollback, want empty", reloaded.Resolution)
	}
	if reloaded.DecidedAt != nil {
		t.Errorf("decided_at = %v after rollback, want NULL", *reloaded.DecidedAt)
	}

	// Assertion 2: NO outbox row carries this dispute's id. The Append
	// inside the rolled-back tx must not have left a trace, otherwise the
	// relay would later ship a DisputeResolved event for a dispute that
	// the DB still reports as open (the exact split-brain S89 fixes).
	var rows int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'DisputeID' = $2`,
		(event.DisputeResolved{}).EventName(), d.ID.String(),
	).Scan(&rows); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if rows != 0 {
		t.Fatalf("outbox rows for dispute %s after rollback = %d, want 0 (Append must have rolled back)", d.ID, rows)
	}
}

// Compile-time guard: the test file references the dispute domain package and
// the event constructor; keep an explicit reference so a future "unused import"
// reflow doesn't accidentally strip them.
var _ = dispute.KindRefund
var _ = (event.DisputeResolved{}).EventName
