package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/shared"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
)

// S123 — Postgres integration tests for the Payment UoW. The
// application-layer / memory tests (see backend/internal/application/payment/
// service_test.go) already prove that paymentapp.Service routes
// authorize/capture/refund writes through the UoW (see subscriber.go
// persistCreate / persistUpdate); this file closes the gap by exercising
// the actual pgx.Tx-bound PaymentRepository so the payments INSERT and the
// outbox INSERT really do commit (or roll back) together against a live
// Postgres.
//
// Each test:
//   - skips cleanly when POSTGRES_TEST_DSN is unset (the suite stays green
//     for dev machines without a database)
//   - reuses the withTestDB / newTestBooking helpers from
//     integration_test.go (which already truncates bookings — payments and
//     payment_adjustments cascade through the FK chain, so this file does
//     not need to truncate them explicitly)
//   - rolls its own UnitOfWork against the live pool so the closure sees a
//     pgx.Tx-bound PaymentRepository the same way paymentapp.persistCreate
//     does
//
// Event-count rationale: investigation of
// backend/internal/application/payment/subscriber.go (authorize at the top
// of the file) shows the authorize UoW emits EXACTLY ONE event —
// PaymentAuthorized — and performs exactly ONE payments INSERT. So the
// atomicity contract this file protects is: one payments INSERT + one
// PaymentAuthorized outbox row, committed or rolled back together.

// newTestPayment builds an authorized payment aggregate for a booking. We
// drive the aggregate through its real Authorize transition so the row is
// representative of what subscriber.authorize lands on a happy path
// (StatusAuthorized + a gateway reference).
func newTestPayment(t *testing.T, bookingID, guestID uuid.UUID) *payment.Payment {
	t.Helper()
	amount, err := shared.NewMoney(10000, "EUR")
	if err != nil {
		t.Fatalf("new money: %v", err)
	}
	p := payment.New(bookingID, guestID, amount)
	if err := p.Authorize("test:" + uuid.NewString()); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	return p
}

// TestPaymentUoW_AuthorizeWritesAtomicallyWithOutbox_Integration — the happy
// path. We seed a booking row (the FK target for the payments row), then
// run the UoW exactly as subscriber.authorize does: tx.Payments.Create the
// authorized payment, append a single PaymentAuthorized record to
// tx.Outbox. After commit the payments row must be present, AND the outbox
// must hold exactly one matching pending event.
func TestPaymentUoW_AuthorizeWritesAtomicallyWithOutbox_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	// Seed the FK chain: host + property + guest + booking. The booking
	// row is the FK target for the payments row we'll write inside the
	// UoW. We persist it OUTSIDE the UoW so the UoW under test performs
	// only the payments INSERT (subscriber.authorize's actual shape).
	b, _ := newTestBooking(t, pool)
	if err := pg.NewBookingRepository(pool).Create(ctx, b); err != nil {
		t.Fatalf("seed booking: %v", err)
	}

	// Capture baselines BEFORE the UoW so we assert only NEW rows are
	// counted. withTestDB truncates bookings + outbox (cascading to
	// payments + payment_adjustments), so these baselines are 0 in
	// practice — pinning them explicitly keeps the test resilient
	// against any future helper that seeds rows during setup.
	var outboxBefore, paymentsBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM payments WHERE booking_id = $1`, b.ID,
	).Scan(&paymentsBefore); err != nil {
		t.Fatalf("baseline payments count: %v", err)
	}

	p := newTestPayment(t, b.ID, b.GuestID)
	uow := pg.NewUnitOfWork(pool, nil)
	if err := uow.Run(ctx, func(tx port.Tx) error {
		if tx.Payments == nil {
			t.Fatalf("tx.Payments is nil — the UoW factory must wire NewPaymentTxRepository")
		}
		if err := tx.Payments.Create(ctx, p); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.PaymentAuthorized{
			BookingID:  p.BookingID,
			PaymentID:  p.ID,
			GuestID:    p.GuestID,
			GatewayRef: p.GatewayRef,
		})
		if err != nil {
			return err
		}
		return tx.Outbox.Append(ctx, rec)
	}); err != nil {
		t.Fatalf("uow run: %v", err)
	}

	// Assertion 1: exactly ONE new payments row landed for this booking.
	var paymentsAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM payments WHERE booking_id = $1`, b.ID,
	).Scan(&paymentsAfter); err != nil {
		t.Fatalf("payments count: %v", err)
	}
	if paymentsAfter-paymentsBefore != 1 {
		t.Fatalf("payments rows for booking %s grew by %d, want 1 (the UoW commit must have persisted the Create write)",
			b.ID, paymentsAfter-paymentsBefore)
	}

	// Belt-and-braces: the specific payment id we built reloads with the
	// gateway ref intact. Guards against a hypothetical case where SOME
	// payment row landed (passing assertion 1) but not the one we wrote.
	reloaded, err := pg.NewPaymentRepository(pool).FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("reload payment: %v", err)
	}
	if reloaded.GatewayRef != p.GatewayRef {
		t.Errorf("reloaded payment gateway ref = %q, want %q", reloaded.GatewayRef, p.GatewayRef)
	}
	if reloaded.Status != payment.StatusAuthorized {
		t.Errorf("reloaded payment status = %q, want authorized", reloaded.Status)
	}

	// Assertion 2: exactly ONE new outbox row tied to this booking, in
	// pending state. The authorize path emits exactly one event
	// (PaymentAuthorized). If a future change adds a second event this
	// assertion will fail loudly so the test stays honest.
	var pendingForBooking int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'BookingID' = $2
		   AND processed_at IS NULL
		   AND failed_at IS NULL
		   AND dead_lettered_at IS NULL`,
		(event.PaymentAuthorized{}).EventName(), b.ID.String(),
	).Scan(&pendingForBooking); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if pendingForBooking != 1 {
		t.Fatalf("pending PaymentAuthorized rows for booking %s = %d, want 1 (the UoW commit must have appended exactly one PaymentAuthorized record)",
			b.ID, pendingForBooking)
	}

	// Assertion 3: the OVERALL outbox grew by exactly 1. Guards against a
	// future change that adds an extra event but happens to use a
	// different event_name or payload key. Together with assertion 2 this
	// pins the contract: one and only one new outbox row per authorize.
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter-outboxBefore != 1 {
		t.Fatalf("outbox grew by %d after authorize, want 1 (the authorize UoW currently emits exactly one event — update this test if subscriber.authorize starts emitting more)",
			outboxAfter-outboxBefore)
	}
}

// TestPaymentUoW_RollbackOnError_Integration — same setup, but the UoW
// closure returns an error after both writes. After rollback the payments
// table must hold ZERO rows for this booking (baseline 0) AND no NEW
// outbox rows may carry this booking's id. This is the contract the UoW
// exists to enforce — without it, a partial commit could ship a
// PaymentAuthorized event for a payment that was never persisted, or
// persist a payment while silently dropping the auto-confirm event.
func TestPaymentUoW_RollbackOnError_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	b, _ := newTestBooking(t, pool)
	if err := pg.NewBookingRepository(pool).Create(ctx, b); err != nil {
		t.Fatalf("seed booking: %v", err)
	}

	var outboxBefore, paymentsBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM payments WHERE booking_id = $1`, b.ID,
	).Scan(&paymentsBefore); err != nil {
		t.Fatalf("baseline payments count: %v", err)
	}

	p := newTestPayment(t, b.ID, b.GuestID)

	wantErr := errors.New("simulated mid-tx failure after payment + outbox write")
	uow := pg.NewUnitOfWork(pool, nil)
	err := uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.Payments.Create(ctx, p); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.PaymentAuthorized{
			BookingID:  p.BookingID,
			PaymentID:  p.ID,
			GuestID:    p.GuestID,
			GatewayRef: p.GatewayRef,
		})
		if err != nil {
			return err
		}
		if err := tx.Outbox.Append(ctx, rec); err != nil {
			return err
		}
		return wantErr // force rollback AFTER all writes
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("uow run err = %v, want %v", err, wantErr)
	}

	// Assertion 1: payments count for this booking is unchanged from the
	// baseline. The Create INSERT inside the failed UoW must have rolled
	// back with the transaction.
	var paymentsAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM payments WHERE booking_id = $1`, b.ID,
	).Scan(&paymentsAfter); err != nil {
		t.Fatalf("payments count: %v", err)
	}
	if paymentsAfter != paymentsBefore {
		t.Fatalf("payments rows for booking %s after rollback = %d, want %d (Create must have rolled back)",
			b.ID, paymentsAfter, paymentsBefore)
	}

	// Belt-and-braces: the specific payment id is not findable. Catches
	// a hypothetical bug where some other payment landed but ours did
	// not (or vice versa) — the count check alone could mask that.
	var ourPaymentRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM payments WHERE id = $1`, p.ID,
	).Scan(&ourPaymentRows); err != nil {
		t.Fatalf("count by payment id: %v", err)
	}
	if ourPaymentRows != 0 {
		t.Fatalf("payments row for our id %s after rollback = %d, want 0 (the rolled-back Create must leave no trace)",
			p.ID, ourPaymentRows)
	}

	// Assertion 2: NO new outbox row carries this booking id. The
	// Append inside the rolled-back tx must not have left a trace,
	// otherwise the relay would later ship a PaymentAuthorized event for
	// a payment that does not exist in the DB (the exact split-brain the
	// UoW exists to prevent — and the bug class S123 closes).
	var leakedForBooking int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'BookingID' = $2`,
		(event.PaymentAuthorized{}).EventName(), b.ID.String(),
	).Scan(&leakedForBooking); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if leakedForBooking != 0 {
		t.Fatalf("PaymentAuthorized rows for booking %s after rollback = %d, want 0 (Append must have rolled back)",
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

// Compile-time guards: this test references the payment domain package
// and the PaymentAuthorized event constructor; keep explicit references
// so a future "unused import" reflow doesn't accidentally strip them.
var _ = payment.New
var _ = (event.PaymentAuthorized{}).EventName
