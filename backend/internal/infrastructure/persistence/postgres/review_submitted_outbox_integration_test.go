package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	reviewapp "github.com/airhost/backend/internal/application/review"
	"github.com/airhost/backend/internal/domain/review"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
)

// S153 — Postgres integration tests for the ReviewSubmitted outbox
// durability path. The application-layer / memory tests in
// backend/internal/application/review/service_test.go already prove that
// reviewapp.Service.Create publishes the ReviewSubmitted event through
// the in-memory UoW; the existing S136 file (review_uow_integration_test.go)
// proves the tx-bound ReviewRepository path against a live Postgres. This
// file closes the verification gap from S136/S147/S148 with a focused,
// service-driven pair of tests that mirror the S143 pattern:
//
//   - Test 1 drives reviewapp.Service.Create end-to-end (NOT through a raw
//     uow.Run) so the assertion proves the production code path commits
//     the reviews row + the ReviewSubmitted outbox event atomically.
//   - Test 2 forces a rollback by failing the closure AFTER both writes
//     and asserts that neither side leaks.
//
// Each test skips cleanly when POSTGRES_TEST_DSN is unset (the suite stays
// green for dev machines without a database) and reuses the shared
// withTestDBReviews / seedHostPropertyGuestBooking helpers from
// review_uow_integration_test.go (both files are package postgres_test).
//
// Event-name rationale: investigation of
// backend/internal/application/event/events.go shows
// ReviewSubmitted{}.EventName() returns "review.submitted" — the assertions
// resolve the name through the constructor (not a string literal) so a
// future rename surfaces here as a compile/runtime mismatch and not as a
// silent regression.

// TestReviewSubmitted_PersistedAtomicallyWithReview — the happy path:
// drive reviewapp.Service.Create with a completed-booking + guest, then
// assert exactly ONE reviews row keyed on (booking_id, author_id) and
// exactly ONE outbox row keyed on event_name = "review.submitted"
// carrying the matching review id + booking id + author id. The payload
// shape is the same one the in-memory test in
// backend/internal/application/review/service_test.go exercises — so
// this test pins the postgres encoding of that exact shape.
func TestReviewSubmitted_PersistedAtomicallyWithReview(t *testing.T) {
	pool := withTestDBReviews(t)
	ctx := context.Background()

	b, prop := seedHostPropertyGuestBooking(t, pool)

	// Baselines captured BEFORE the use case runs. withTestDBReviews
	// truncates everything, so these are 0 in practice — pinning them
	// keeps the test resilient against any future helper that seeds rows
	// during setup.
	var outboxBefore, reviewsBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM reviews WHERE booking_id = $1`, b.ID,
	).Scan(&reviewsBefore); err != nil {
		t.Fatalf("baseline reviews count: %v", err)
	}

	// Drive the application service end-to-end through a UoW so we
	// exercise the same persistCreate path the HTTP handler runs in
	// production. Without WithUnitOfWork the service falls back to the
	// pool-bound repo with NO outbox event — which is the very gap this
	// slice locks shut.
	svc := reviewapp.NewService(
		pg.NewReviewRepository(pool),
		pg.NewBookingRepository(pool),
		pg.NewPropertyRepository(pool),
	).WithUnitOfWork(pg.NewUnitOfWork(pool, nil)) // relay nil — we read the outbox table directly

	rv, err := svc.Create(ctx, reviewapp.CreateInput{
		GuestID:   b.GuestID,
		BookingID: b.ID,
		Rating:    5,
		Comment:   "Lovely stay",
	})
	if err != nil {
		t.Fatalf("review create: %v", err)
	}

	// Assertion 1: exactly ONE reviews row landed for this booking, keyed
	// on (booking_id, author_id). Pinning author_id alongside the booking
	// id guards against a hypothetical future bug where the service writes
	// SOME review row but with the wrong author (e.g. a sloppy refactor
	// that mis-wires the host vs guest path).
	var rowsForBookingAndAuthor int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM reviews WHERE booking_id = $1 AND author_id = $2`,
		b.ID, b.GuestID,
	).Scan(&rowsForBookingAndAuthor); err != nil {
		t.Fatalf("reviews count by (booking, author): %v", err)
	}
	if rowsForBookingAndAuthor != 1 {
		t.Fatalf("reviews rows for (booking=%s, author=%s) = %d, want 1 (the UoW commit must have persisted the Create write)",
			b.ID, b.GuestID, rowsForBookingAndAuthor)
	}

	// Belt-and-braces: the specific review id the service returned reloads
	// with the expected (booking, author, property) tuple. Guards against a
	// case where assertion 1 sees a row that happened to match the tuple
	// but isn't the one Create returned.
	reloaded, err := pg.NewReviewRepository(pool).FindByID(ctx, rv.ID)
	if err != nil {
		t.Fatalf("reload review: %v", err)
	}
	if reloaded.BookingID != b.ID {
		t.Errorf("reloaded review booking = %s, want %s", reloaded.BookingID, b.ID)
	}
	if reloaded.AuthorID != b.GuestID {
		t.Errorf("reloaded review author = %s, want %s", reloaded.AuthorID, b.GuestID)
	}
	if reloaded.PropertyID != prop.ID {
		t.Errorf("reloaded review property = %s, want %s", reloaded.PropertyID, prop.ID)
	}
	if reloaded.Kind != review.KindGuestToProperty {
		t.Errorf("reloaded review kind = %q, want %q", reloaded.Kind, review.KindGuestToProperty)
	}

	// Assertion 2: exactly ONE outbox row keyed by event_name
	// "review.submitted" (resolved through ReviewSubmitted{}.EventName so
	// a rename of the constant doesn't slip past) carries this review id +
	// booking id, in pending state.
	var pendingForReview int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'ReviewID' = $2
		   AND payload->>'BookingID' = $3
		   AND processed_at IS NULL
		   AND failed_at IS NULL
		   AND dead_lettered_at IS NULL`,
		(event.ReviewSubmitted{}).EventName(), rv.ID.String(), b.ID.String(),
	).Scan(&pendingForReview); err != nil {
		t.Fatalf("outbox count by (event, review, booking): %v", err)
	}
	if pendingForReview != 1 {
		t.Fatalf("pending %q rows for review=%s booking=%s = %d, want 1 (the UoW commit must have appended exactly one record)",
			(event.ReviewSubmitted{}).EventName(), rv.ID, b.ID, pendingForReview)
	}

	// Assertion 3: the OVERALL outbox grew by exactly 1. Guards against a
	// future change that adds an extra event under a different event_name
	// or payload key (so assertion 2 would still see 1).
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter-outboxBefore != 1 {
		t.Fatalf("outbox grew by %d after review create, want 1 (the Create UoW currently emits exactly one event — update this test if persistCreate starts emitting more)",
			outboxAfter-outboxBefore)
	}

	// Assertion 4: the payload field shape matches what the subscribers
	// (notification BC + cached-rating recalc + Superhost re-evaluation)
	// read. We decode into ReviewSubmitted to round-trip through the same
	// JSON encoder events.Register uses on the consumer side, so a future
	// field rename surfaces here as a value mismatch and not as a silent
	// data-shape drift.
	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT payload FROM outbox
		 WHERE event_name = $1
		   AND payload->>'ReviewID' = $2`,
		(event.ReviewSubmitted{}).EventName(), rv.ID.String(),
	).Scan(&payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	var decoded event.ReviewSubmitted
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded.ReviewID != rv.ID {
		t.Errorf("payload.ReviewID = %s, want %s", decoded.ReviewID, rv.ID)
	}
	if decoded.BookingID != b.ID {
		t.Errorf("payload.BookingID = %s, want %s", decoded.BookingID, b.ID)
	}
	if decoded.PropertyID != prop.ID {
		t.Errorf("payload.PropertyID = %s, want %s", decoded.PropertyID, prop.ID)
	}
	if decoded.AuthorID != b.GuestID {
		t.Errorf("payload.AuthorID = %s, want %s", decoded.AuthorID, b.GuestID)
	}
	if decoded.GuestID != b.GuestID {
		t.Errorf("payload.GuestID = %s, want %s", decoded.GuestID, b.GuestID)
	}
	// Direction string equals the review.Kind so subscribers can switch
	// on a plain string without importing the review domain. For a
	// guest-to-property review it must be "guest_to_property".
	if decoded.Direction != string(review.KindGuestToProperty) {
		t.Errorf("payload.Direction = %q, want %q", decoded.Direction, string(review.KindGuestToProperty))
	}
	if decoded.Rating != 5 {
		t.Errorf("payload.Rating = %d, want 5", decoded.Rating)
	}
}

// TestReviewSubmitted_RollbackLeavesNoRowOrEvent — same setup, but the
// closure returns an error AFTER both writes succeeded (the reviews
// INSERT and the outbox Append are both syntactically valid; we force
// the rollback by returning a sentinel error from the closure). After
// rollback:
//   - SELECT COUNT(*) FROM reviews WHERE id = <our id> must be 0
//   - SELECT COUNT(*) FROM outbox WHERE event_name = 'review.submitted'
//     AND the review we tried to write must be 0
//
// We drive the UoW directly (not through reviewapp.Service.Create) because
// the service does not expose a hook to inject a mid-tx failure — and
// rebuilding it for the test would re-implement the very contract this
// test exists to lock down. The S143 sibling
// (kyc_stepup_uow_integration_test.go) takes the inverse approach (the
// gate-deny path is built into the service itself, so it can drive end-
// to-end); the review path has no equivalent natural failure mode, so we
// take the standard "return an error from the closure" technique used by
// S114 (dispute_open_uow_integration_test.go) and S136
// (review_uow_integration_test.go), which is the cleanest way to inject
// a failure into the UoW once both writes are on the wire.
func TestReviewSubmitted_RollbackLeavesNoRowOrEvent(t *testing.T) {
	pool := withTestDBReviews(t)
	ctx := context.Background()

	b, prop := seedHostPropertyGuestBooking(t, pool)

	var outboxBefore, reviewsBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM reviews WHERE booking_id = $1`, b.ID,
	).Scan(&reviewsBefore); err != nil {
		t.Fatalf("baseline reviews count: %v", err)
	}

	rv, err := review.NewPropertyReview(b.ID, prop.ID, b.GuestID, 4, "Great")
	if err != nil {
		t.Fatalf("new review: %v", err)
	}

	wantErr := errors.New("S153 simulated mid-tx failure after review + outbox write")
	uow := pg.NewUnitOfWork(pool, nil)
	err = uow.Run(ctx, func(tx port.Tx) error {
		if tx.Reviews == nil {
			t.Fatalf("tx.Reviews is nil — the UoW factory must wire NewReviewTxRepository")
		}
		// Both writes happen inside the tx; both must roll back.
		if err := tx.Reviews.Create(ctx, rv); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.ReviewSubmitted{
			ReviewID:   rv.ID,
			BookingID:  rv.BookingID,
			PropertyID: rv.PropertyID,
			AuthorID:   rv.AuthorID,
			GuestID:    rv.GuestID,
			Direction:  string(rv.Kind),
			Rating:     rv.Rating,
		})
		if err != nil {
			return err
		}
		if err := tx.Outbox.Append(ctx, rec); err != nil {
			return err
		}
		// Force rollback AFTER both writes — this is the half of the
		// atomicity contract no application-layer test can prove (the
		// in-memory UoW commits map writes immediately; rollback is a
		// real-tx-only behaviour).
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("uow run err = %v, want %v", err, wantErr)
	}

	// Assertion 1 (the exact assertion the task spec calls out): the
	// reviews row for our id is gone — SELECT COUNT(*) FROM reviews
	// WHERE id = $1 must be 0.
	var rowsForID int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM reviews WHERE id = $1`, rv.ID,
	).Scan(&rowsForID); err != nil {
		t.Fatalf("reviews count by id: %v", err)
	}
	if rowsForID != 0 {
		t.Fatalf("reviews row for id %s after rollback = %d, want 0 (the rolled-back Create must leave no trace)",
			rv.ID, rowsForID)
	}

	// Belt-and-braces: the bookings-keyed count is unchanged from the
	// baseline. Catches a hypothetical bug where SOME different review id
	// landed for this booking even though our specific id did not.
	var rowsForBooking int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM reviews WHERE booking_id = $1`, b.ID,
	).Scan(&rowsForBooking); err != nil {
		t.Fatalf("reviews count by booking: %v", err)
	}
	if rowsForBooking != reviewsBefore {
		t.Fatalf("reviews rows for booking %s after rollback = %d, want %d",
			b.ID, rowsForBooking, reviewsBefore)
	}

	// Assertion 2 (the exact assertion the task spec calls out): no outbox
	// row exists for event_name 'review.submitted' carrying our review id.
	// If the Append survived the rolled-back tx, the relay would later
	// ship a ReviewSubmitted event for a review that does not exist in the
	// DB — the exact split-brain S136 closes and this slice verifies.
	var leakedForReview int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'ReviewID' = $2`,
		(event.ReviewSubmitted{}).EventName(), rv.ID.String(),
	).Scan(&leakedForReview); err != nil {
		t.Fatalf("outbox count by (event, review): %v", err)
	}
	if leakedForReview != 0 {
		t.Fatalf("%q rows for review %s after rollback = %d, want 0 (Append must have rolled back)",
			(event.ReviewSubmitted{}).EventName(), rv.ID, leakedForReview)
	}

	// Assertion 3: the TOTAL outbox row count is unchanged. Belt-and-
	// braces against a future change to event payload field names (so the
	// payload->>ReviewID filter in assertion 2 misses) AND against a
	// future helper that seeds outbox state during setup.
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter != outboxBefore {
		t.Fatalf("outbox count after rollback = %d, want %d (no NEW rows must have leaked through the rolled-back tx)",
			outboxAfter, outboxBefore)
	}
}

// Compile-time guards: this test references the review domain package,
// the ReviewSubmitted event constructor, and the reviewapp service so a
// future "unused import" reflow doesn't accidentally strip them.
var _ = review.NewPropertyReview
var _ = (event.ReviewSubmitted{}).EventName
var _ = reviewapp.NewService
