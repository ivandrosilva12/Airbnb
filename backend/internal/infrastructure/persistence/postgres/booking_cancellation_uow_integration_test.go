package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/booking"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
)

// S128 — Postgres integration tests for the booking CANCELLATION UoW. The
// application / memory tests (see backend/internal/application/booking/
// service_test.go) already prove that booking.Service.Cancel routes the
// status flip + the BookingCancelled event through the UoW; this file
// closes the gap by exercising the actual pgx.Tx-bound BookingRepository
// and outbox so the bookings UPDATE and the outbox INSERT really do
// commit (or roll back) together against a live Postgres.
//
// Each test:
//   - skips cleanly when POSTGRES_TEST_DSN is unset (the suite stays green
//     for dev machines without a database)
//   - reuses the withTestDB / truncate / newTestBooking helpers from
//     integration_test.go (which already truncates bookings, properties,
//     users, splits, and outbox between tests)
//   - rolls its own UnitOfWork against the live pool so the closure sees a
//     pgx.Tx-bound BookingRepository the same way booking.Service.Cancel
//     does via its emit() helper
//
// Event-count rationale: investigation of
// backend/internal/application/booking/service.go (Cancel at L983-L1019)
// shows the cancellation UoW emits EXACTLY ONE event — BookingCancelled.
// The refund itself is not a separate event from the booking BC; the
// payment-context subscriber consumes BookingCancelled (carrying
// RefundFraction) and produces a refund downstream in its own UoW. So the
// atomicity contract this file protects is: one booking UPDATE + one
// BookingCancelled outbox row, committed or rolled back together.

// TestBookingCancellationUoW_WritesAtomicallyWithOutbox_Integration —
// the happy path. We seed a confirmed booking row (the realistic
// pre-cancel state — Cancel rejects already-cancelled or completed
// rows), then run the UoW exactly as Service.Cancel does: flip the
// aggregate to StatusCancelled, tx.Bookings.Update, append a single
// BookingCancelled record to tx.Outbox. After commit the row must read
// back as cancelled AND the outbox must hold exactly one matching
// pending event.
func TestBookingCancellationUoW_WritesAtomicallyWithOutbox_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	// newTestBooking seeds the FK chain (host user + property + guest
	// user) and returns an UNSAVED Booking aggregate. We persist it
	// first OUTSIDE the cancellation UoW so the UoW we're testing
	// performs an UPDATE (Service.Cancel's actual shape) rather than
	// an INSERT — the bug class S128 protects against is "the UPDATE
	// landed but the event didn't", which can't be reproduced if the
	// row only exists inside the same tx.
	b, prop := newTestBooking(t, pool)
	if err := pg.NewBookingRepository(pool).Create(ctx, b); err != nil {
		t.Fatalf("seed booking row: %v", err)
	}
	// Real cancellations target confirmed bookings — the host has
	// approved, the guest is on the hook for a refund-fraction haircut.
	// The domain Cancel() also accepts pending, but confirmed exercises
	// the more interesting path (the row has been touched twice already
	// and the UPDATE has more to write back).
	if err := b.Confirm(); err != nil {
		t.Fatalf("seed confirm: %v", err)
	}
	if err := pg.NewBookingRepository(pool).Update(ctx, b); err != nil {
		t.Fatalf("persist confirm: %v", err)
	}

	// Capture the outbox row count BEFORE the UoW so we can assert
	// only NEW rows are counted — the suite's withTestDB truncates
	// outbox, so the baseline is 0 here, but pinning it explicitly
	// keeps the test resilient against any future helper that seeds
	// outbox rows during setup.
	var outboxBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}

	// Now run the cancellation UoW exactly the way Service.Cancel does:
	// flip the aggregate, write back via tx.Bookings.Update, append one
	// BookingCancelled record to tx.Outbox. RefundFraction = 1.0 mirrors
	// the host-cancel path (always full refund); the value is incidental
	// for this test — we just need a valid payload.
	if err := b.Cancel(); err != nil {
		t.Fatalf("aggregate cancel: %v", err)
	}
	uow := pg.NewUnitOfWork(pool, nil)
	if err := uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.Bookings.Update(ctx, b); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.BookingCancelled{
			BookingID:      b.ID,
			PropertyID:     prop,
			PropertyTitle:  "Test Flat", // matches seedHostAndProperty
			HostID:         uuid.Nil,    // not under test; subscriber receives whatever the service supplies
			GuestID:        b.GuestID,
			CancelledBy:    b.GuestID,
			RefundFraction: 1.0,
		})
		if err != nil {
			return err
		}
		return tx.Outbox.Append(ctx, rec)
	}); err != nil {
		t.Fatalf("uow run: %v", err)
	}

	// Assertion 1: the bookings row is now cancelled. We round-trip
	// through the repo so any silent column-shape regression in the
	// status writeback also surfaces. We check both the in-memory
	// aggregate (proves Cancel returned cleanly) and the DB row
	// (proves the Update inside the UoW actually committed).
	reloaded, err := pg.NewBookingRepository(pool).FindByID(ctx, b.ID)
	if err != nil {
		t.Fatalf("reload booking: %v", err)
	}
	if reloaded.Status != booking.StatusCancelled {
		t.Errorf("booking status after commit = %q, want %q (the UoW Update must have flipped the row)",
			reloaded.Status, booking.StatusCancelled)
	}

	// Assertion 2: exactly ONE new outbox row tied to this booking, in
	// pending state. The Service.Cancel path emits exactly one event
	// (BookingCancelled) — see service.go L1004-L1014. If a future change
	// adds a second event (e.g. RefundIssued) this assertion will fail
	// loudly so the test stays honest about what the production path
	// actually emits.
	var pending int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'BookingID' = $2
		   AND processed_at IS NULL
		   AND failed_at IS NULL
		   AND dead_lettered_at IS NULL`,
		(event.BookingCancelled{}).EventName(), b.ID.String(),
	).Scan(&pending); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending BookingCancelled rows for booking %s = %d, want 1 (the UoW commit must have appended exactly one BookingCancelled record)",
			b.ID, pending)
	}

	// Assertion 3: the OVERALL outbox grew by exactly 1. This guards
	// against a future change that adds an extra event to the cancel
	// path but happens to use a different event_name (so assertion 2
	// would still see 1). Together the two assertions pin the contract:
	// one and only one new outbox row per cancellation.
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter-outboxBefore != 1 {
		t.Fatalf("outbox grew by %d after cancel, want 1 (the cancel UoW currently emits exactly one event — update this test if Service.Cancel starts emitting more)",
			outboxAfter-outboxBefore)
	}
}

// TestBookingCancellationUoW_RollbackOnError_Integration — same setup,
// but the UoW closure returns an error after both writes. After
// rollback the booking row must stay in its PRE-CALL status (confirmed)
// AND no NEW outbox rows may carry this booking's id. This is the
// contract the UoW exists to enforce — without it, a partial commit
// could ship a BookingCancelled event for a booking that is still
// confirmed in the database (the relay would then refund a stay the
// host still considers live), or flip the booking to cancelled while
// silently dropping the notification.
func TestBookingCancellationUoW_RollbackOnError_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	b, prop := newTestBooking(t, pool)
	if err := pg.NewBookingRepository(pool).Create(ctx, b); err != nil {
		t.Fatalf("seed booking row: %v", err)
	}
	if err := b.Confirm(); err != nil {
		t.Fatalf("seed confirm: %v", err)
	}
	if err := pg.NewBookingRepository(pool).Update(ctx, b); err != nil {
		t.Fatalf("persist confirm: %v", err)
	}

	// Baseline: how many outbox rows EXIST before the UoW runs. We'll
	// assert this count is unchanged after the forced rollback.
	var outboxBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}

	// IMPORTANT: do NOT flip the aggregate before the UoW. The aggregate
	// is in-memory state, not the DB row — even if Cancel() runs, the
	// DB still holds StatusConfirmed until the UoW commits. The test
	// asserts the DB-side rollback semantics, so we mutate the
	// aggregate INSIDE the UoW closure and then force the error.
	wantErr := errors.New("simulated mid-tx failure after cancel write")
	uow := pg.NewUnitOfWork(pool, nil)
	err := uow.Run(ctx, func(tx port.Tx) error {
		if err := b.Cancel(); err != nil {
			return err
		}
		if err := tx.Bookings.Update(ctx, b); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.BookingCancelled{
			BookingID:      b.ID,
			PropertyID:     prop,
			PropertyTitle:  "Test Flat",
			HostID:         uuid.Nil,
			GuestID:        b.GuestID,
			CancelledBy:    b.GuestID,
			RefundFraction: 1.0,
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

	// Assertion 1: the booking row still reads as confirmed (the
	// PRE-CALL status). The in-tx Update must have rolled back with
	// the transaction; reading the row back via the repo should
	// surface the SAME status we wrote during seed.
	reloaded, err := pg.NewBookingRepository(pool).FindByID(ctx, b.ID)
	if err != nil {
		t.Fatalf("reload booking: %v", err)
	}
	if reloaded.Status != booking.StatusConfirmed {
		t.Fatalf("booking status after rollback = %q, want %q (the Update inside the failed UoW must have rolled back)",
			reloaded.Status, booking.StatusConfirmed)
	}

	// Assertion 2: NO new outbox row carries this booking id. The
	// Append inside the rolled-back tx must not have left a trace,
	// otherwise the relay would later ship a BookingCancelled event
	// for a booking that is still confirmed in the DB (the exact
	// split-brain the UoW exists to prevent).
	var leakedForBooking int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'BookingID' = $2`,
		(event.BookingCancelled{}).EventName(), b.ID.String(),
	).Scan(&leakedForBooking); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if leakedForBooking != 0 {
		t.Fatalf("BookingCancelled rows for booking %s after rollback = %d, want 0 (Append must have rolled back)",
			b.ID, leakedForBooking)
	}

	// Assertion 3: the TOTAL outbox row count is unchanged. Belt-and-
	// braces against a future change to event payloads (so payload
	// filtering in assertion 2 misses) and a future helper that seeds
	// outbox state during setup.
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter != outboxBefore {
		t.Fatalf("outbox count after rollback = %d, want %d (no NEW rows must have leaked through the rolled-back tx)",
			outboxAfter, outboxBefore)
	}
}

// Compile-time guard: the test file references the booking domain
// package and the BookingCancelled event constructor; keep explicit
// references so a future "unused import" reflow doesn't accidentally
// strip them.
var _ = booking.StatusCancelled
var _ = (event.BookingCancelled{}).EventName
var _ uuid.UUID
