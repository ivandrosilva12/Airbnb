package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
)

// S119 — Postgres integration tests for the booking + split-payment Create
// flow inside a UnitOfWork (S35 / WF-GAP cluster around split-pay seeding).
// The application-layer / memory tests (see
// backend/internal/application/splitpayment/service_test.go) already prove
// that bookingapp routes a split-pay seed through tx.SplitPayments.Create
// alongside the booking write; this file closes the gap by exercising the
// actual pgx.Tx-bound SplitPaymentRepository so the bookings INSERT, the
// split_payments INSERT, AND the per-share split_payment_shares INSERTs
// really do commit (or roll back) together against a live Postgres.
//
// Each test:
//   - skips cleanly when POSTGRES_TEST_DSN is unset (the suite stays green
//     for dev machines without a database)
//   - reuses the withTestDB / truncate / newTestBooking helpers from
//     integration_test.go (which already truncates bookings,
//     split_payments and split_payment_shares between tests)
//   - rolls its own UnitOfWork against the live pool so the closure sees a
//     pgx.Tx-bound SplitPaymentRepository the same way bookingapp does
//     post-S35
//
// The sibling test TestIntegration_SplitPayment_TxRepository_AtomicWithOutbox
// in integration_test.go covers the same UoW with an outbox append on top;
// these tests focus narrowly on the booking + split atomicity contract so
// a future change to event payloads can't accidentally mask a regression in
// the underlying aggregate write.

// TestSplitPaymentUoW_CreateWritesAtomicallyWithBooking_Integration — the
// happy path. Inside a UoW we (a) Create a fresh Booking aggregate via
// tx.Bookings.Create AND (b) Create a SplitPayment carrying TWO shares via
// tx.SplitPayments.Create. After commit, all three row sets must be present:
// the bookings row, the split_payments parent, AND exactly two
// split_payment_shares tied to it. The in-tx contract S35 guarantees this
// can never partially land — a crash mid-flow leaves no booking either.
func TestSplitPaymentUoW_CreateWritesAtomicallyWithBooking_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	// newTestBooking seeds the FK chain (host user + property + guest user)
	// and returns an UNSAVED Booking aggregate so the test controls when /
	// how it gets persisted (via the UoW).
	b, _ := newTestBooking(t, pool)

	// Build a 2-share split that satisfies the aggregate invariants:
	// the organizer must appear as a payer, share sums must match the
	// booking total exactly, currency is normalised to upper case.
	organizerEmail := "organizer-" + uuid.NewString() + "@test.dev"
	friendEmail := "friend-" + uuid.NewString() + "@test.dev"
	sp, err := splitpayment.New(
		b.ID, b.GuestID, organizerEmail, "EUR",
		b.Pricing.Total.AmountCents(),
		[]splitpayment.ShareInput{
			{Email: organizerEmail, AmountCents: b.Pricing.Total.AmountCents() - 4000},
			{Email: friendEmail, AmountCents: 4000},
		},
	)
	if err != nil {
		t.Fatalf("new split: %v", err)
	}

	// Run the UoW: persist the booking + the split (parent row + both
	// shares) inside one transaction. Mirrors what bookingapp.Create does
	// after S35 when the request carries split-pay seed input.
	uow := pg.NewUnitOfWork(pool, nil)
	if err := uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.Bookings.Create(ctx, b); err != nil {
			return err
		}
		return tx.SplitPayments.Create(ctx, sp)
	}); err != nil {
		t.Fatalf("uow run: %v", err)
	}

	// Assertion 1: the bookings row landed. We round-trip through the
	// repository so any silent column-shape regression also surfaces.
	var bookingRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE id = $1`, b.ID,
	).Scan(&bookingRows); err != nil {
		t.Fatalf("bookings count: %v", err)
	}
	if bookingRows != 1 {
		t.Fatalf("bookings rows for %s after commit = %d, want 1", b.ID, bookingRows)
	}

	// Assertion 2: the split_payments parent row landed, linked to the
	// booking by booking_id. We deliberately query by booking_id (not just
	// the split id) so any future change to the FK direction also flags.
	var parentRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM split_payments WHERE id = $1 AND booking_id = $2`,
		sp.ID, b.ID,
	).Scan(&parentRows); err != nil {
		t.Fatalf("split_payments count: %v", err)
	}
	if parentRows != 1 {
		t.Fatalf("split_payments rows for split %s on booking %s after commit = %d, want 1",
			sp.ID, b.ID, parentRows)
	}

	// Assertion 3: split_payment_shares carries EXACTLY two rows for this
	// split, matching the ShareInput count. This is the row count the S35
	// contract specifically protects — a crash between the parent INSERT
	// and the per-share INSERTs would leave a parent with 0 or 1 shares
	// and break completion (the booking can never confirm).
	var shareRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM split_payment_shares WHERE split_payment_id = $1`, sp.ID,
	).Scan(&shareRows); err != nil {
		t.Fatalf("split_payment_shares count: %v", err)
	}
	if shareRows != 2 {
		t.Fatalf("split_payment_shares rows for split %s after commit = %d, want 2 (the UoW commit must have persisted both ShareInputs)",
			sp.ID, shareRows)
	}

	// Belt-and-braces: the split reloads via the repo with both shares
	// attached and the booking id intact. This guards against a case
	// where the rows exist but the parent->shares join is broken (e.g.
	// shares pointing at a different split_payment_id).
	reloaded, err := pg.NewSplitPaymentRepository(pool).FindByID(ctx, sp.ID)
	if err != nil {
		t.Fatalf("reload split: %v", err)
	}
	if reloaded.BookingID != b.ID {
		t.Errorf("reloaded split booking id = %s, want %s", reloaded.BookingID, b.ID)
	}
	if len(reloaded.Shares) != 2 {
		t.Errorf("reloaded split share count = %d, want 2", len(reloaded.Shares))
	}
}

// TestSplitPaymentUoW_RollbackOnError_Integration — same setup, but the
// UoW closure returns an error after both writes. After rollback, NO row
// may exist in any of the three tables for this booking. This is the
// contract S35 exists to enforce — without the UoW, a partial commit
// could leave a split_payments parent (or a half-populated share set)
// for a booking that was never persisted, or vice-versa.
func TestSplitPaymentUoW_RollbackOnError_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	b, _ := newTestBooking(t, pool)

	organizerEmail := "organizer-" + uuid.NewString() + "@test.dev"
	friendEmail := "friend-" + uuid.NewString() + "@test.dev"
	sp, err := splitpayment.New(
		b.ID, b.GuestID, organizerEmail, "EUR",
		b.Pricing.Total.AmountCents(),
		[]splitpayment.ShareInput{
			{Email: organizerEmail, AmountCents: b.Pricing.Total.AmountCents() - 3000},
			{Email: friendEmail, AmountCents: 3000},
		},
	)
	if err != nil {
		t.Fatalf("new split: %v", err)
	}

	wantErr := errors.New("simulated failure after split create")
	uow := pg.NewUnitOfWork(pool, nil)
	err = uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.Bookings.Create(ctx, b); err != nil {
			return err
		}
		if err := tx.SplitPayments.Create(ctx, sp); err != nil {
			return err
		}
		return wantErr // force rollback AFTER both writes
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("uow run err = %v, want %v", err, wantErr)
	}

	// Assertion 1: the bookings row does NOT exist. The in-tx INSERT
	// must have rolled back with the transaction.
	var bookingRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE id = $1`, b.ID,
	).Scan(&bookingRows); err != nil {
		t.Fatalf("bookings count: %v", err)
	}
	if bookingRows != 0 {
		t.Fatalf("bookings rows for %s after rollback = %d, want 0", b.ID, bookingRows)
	}

	// Assertion 2: no split_payments parent persisted for this booking.
	// We query by booking_id rather than the in-memory split id so a
	// future change to id generation can't accidentally mask a survivor
	// (e.g. an INSERT that landed before the rollback fired).
	var parentRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM split_payments WHERE booking_id = $1`, b.ID,
	).Scan(&parentRows); err != nil {
		t.Fatalf("split_payments count: %v", err)
	}
	if parentRows != 0 {
		t.Fatalf("split_payments rows for booking %s after rollback = %d, want 0 (Create must have rolled back)",
			b.ID, parentRows)
	}

	// Assertion 3: no split_payment_shares persisted under this split id.
	// The FK ON DELETE CASCADE from the migration is irrelevant here —
	// nothing committed, so nothing can cascade. We check directly that
	// the per-share INSERT(s) inside the rolled-back tx left no trace.
	var shareRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM split_payment_shares WHERE split_payment_id = $1`, sp.ID,
	).Scan(&shareRows); err != nil {
		t.Fatalf("split_payment_shares count: %v", err)
	}
	if shareRows != 0 {
		t.Fatalf("split_payment_shares rows for split %s after rollback = %d, want 0 (per-share INSERTs must have rolled back)",
			sp.ID, shareRows)
	}

	// Belt-and-braces: the split must not be reachable via the repo's
	// not-found path. If FindByID returns anything other than
	// shared.ErrNotFound, the rollback leaked.
	if _, err := pg.NewSplitPaymentRepository(pool).FindByID(ctx, sp.ID); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("FindByID after rollback err = %v, want shared.ErrNotFound (the Create must have rolled back)", err)
	}
}

// Compile-time guard: the test file references the splitpayment domain
// package; keep an explicit reference so a future "unused import" reflow
// doesn't accidentally strip them.
var _ = splitpayment.SharePending
var _ uuid.UUID
