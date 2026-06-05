package postgres_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	reviewapp "github.com/airhost/backend/internal/application/review"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// S136 — Postgres integration tests for the Review UoW. The application-layer
// / memory tests (see backend/internal/application/review/service_test.go)
// already prove that reviewapp.Service.Create / CreateGuestReview behave
// correctly under the backward-compat fallback (no UoW configured);
// this file closes the gap by exercising the actual pgx.Tx-bound
// ReviewRepository so the reviews INSERT and the outbox INSERT really do
// commit (or roll back) together against a live Postgres.
//
// Each test:
//   - skips cleanly when POSTGRES_TEST_DSN is unset (the suite stays green
//     for dev machines without a database)
//   - rolls its own UnitOfWork against the live pool so the closure sees a
//     pgx.Tx-bound ReviewRepository the same way reviewapp.persistCreate
//     does in production
//   - truncates the FK chain (users + properties cascade to bookings →
//     reviews) plus outbox before each run
//
// Event-count rationale: investigation of
// backend/internal/application/review/service.go (persistCreate) shows the
// Create UoW emits EXACTLY ONE event — ReviewSubmitted — and performs
// exactly ONE reviews INSERT. So the atomicity contract this file
// protects is: one reviews INSERT + one ReviewSubmitted outbox row,
// committed or rolled back together.

const reviewDSNEnv = "POSTGRES_TEST_DSN"

// migrateReviewsOnce makes sure migrations run exactly once per test
// binary even when other integration test files in the same package add
// their own one-shot guards. Each test still truncates the tables it
// touches.
var migrateReviewsOnce sync.Once

// withTestDBReviews returns a pool against the env-configured postgres,
// after running migrations and truncating the tables this file touches.
// The test is skipped when POSTGRES_TEST_DSN is unset, so `go test ./...`
// stays green on dev machines without a database. Named with the
// "Reviews" suffix so the function does not collide with the
// withTestDB helper a future sibling integration_test.go might add
// (the slice doesn't introduce one; that's an out-of-scope refactor).
func withTestDBReviews(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(reviewDSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping postgres integration test", reviewDSNEnv)
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	var migrateErr error
	migrateReviewsOnce.Do(func() {
		dir, err := filepath.Abs("migrations")
		if err != nil {
			migrateErr = err
			return
		}
		migrateErr = pg.Migrate(ctx, pool, dir)
	})
	if migrateErr != nil {
		t.Fatalf("migrate: %v", migrateErr)
	}

	// Truncate the FK chain plus outbox. The reviews / bookings tables FK
	// CASCADE from users + properties, so dropping the parents drops the
	// children; outbox is independent and truncated explicitly.
	for _, tbl := range []string{
		"reviews", "bookings", "properties", "users", "outbox",
	} {
		if _, err := pool.Exec(ctx,
			"TRUNCATE TABLE "+tbl+" RESTART IDENTITY CASCADE",
		); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
	return pool
}

// seedHostPropertyGuestBooking lands the FK chain (host + property +
// guest + a confirmed-then-completed booking) the review use case needs.
// Returns the persisted booking + property so the test can drive the
// service / inspect the rows. The booking is left in StatusCompleted —
// reviewapp.Create rejects anything else.
func seedHostPropertyGuestBooking(t *testing.T, pool *pgxpool.Pool) (*booking.Booking, *property.Property) {
	t.Helper()
	ctx := context.Background()

	host, _ := user.NewUser("kc-host-"+uuid.NewString(), "host-"+uuid.NewString()+"@test.dev", "Host", user.RoleHost)
	users := pg.NewUserRepository(pool)
	if err := users.Create(ctx, host); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	guest, _ := user.NewUser("kc-guest-"+uuid.NewString(), "guest-"+uuid.NewString()+"@test.dev", "Guest", user.RoleGuest)
	if err := users.Create(ctx, guest); err != nil {
		t.Fatalf("seed guest: %v", err)
	}

	addr := property.Address{Line1: "1 Main", City: "Lisbon", Country: "PT"}
	price, _ := shared.NewMoney(10000, "EUR")
	zero, _ := shared.NewMoney(0, "EUR")
	prop, err := property.NewProperty(host.ID, "Test Flat", "desc", property.TypeApartment, addr, price, zero, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	if err := pg.NewPropertyRepository(pool).Create(ctx, prop); err != nil {
		t.Fatalf("seed property: %v", err)
	}

	dates, err := booking.NewDateRange(time.Now().UTC().AddDate(0, 0, -10), time.Now().UTC().AddDate(0, 0, -8))
	if err != nil {
		// future-only dates: NewDateRange may reject past starts depending on
		// validation; fall back to a future window and Complete via manual
		// status flip below.
		dates, err = booking.NewDateRange(time.Now().UTC().AddDate(0, 0, 30), time.Now().UTC().AddDate(0, 0, 32))
		if err != nil {
			t.Fatalf("dates: %v", err)
		}
	}
	b, err := booking.NewBooking(prop.ID, guest.ID, dates, 2, price, zero, 0.10, booking.Discounts{})
	if err != nil {
		t.Fatalf("new booking: %v", err)
	}
	if err := b.Confirm(); err != nil {
		t.Fatalf("confirm booking: %v", err)
	}
	if err := b.Complete(); err != nil {
		t.Fatalf("complete booking: %v", err)
	}
	if err := pg.NewBookingRepository(pool).Create(ctx, b); err != nil {
		t.Fatalf("seed booking: %v", err)
	}
	return b, prop
}

// TestReviewUoW_SubmitWritesAtomicallyWithOutbox_Integration — the happy
// path. We seed a completed booking, drive reviewapp.Service.Create
// (wired to a UoW via WithUnitOfWork) and assert exactly ONE new reviews
// row + exactly ONE new ReviewSubmitted outbox row committed together.
func TestReviewUoW_SubmitWritesAtomicallyWithOutbox_Integration(t *testing.T) {
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

	// Drive the application service end-to-end so the test exercises
	// reviewapp.persistCreate — which is what production goes through.
	svc := reviewapp.NewService(
		pg.NewReviewRepository(pool),
		pg.NewBookingRepository(pool),
		pg.NewPropertyRepository(pool),
	).WithUnitOfWork(pg.NewUnitOfWork(pool, nil))
	rv, err := svc.Create(ctx, reviewapp.CreateInput{
		GuestID:   b.GuestID,
		BookingID: b.ID,
		Rating:    5,
		Comment:   "Lovely stay",
	})
	if err != nil {
		t.Fatalf("review create: %v", err)
	}

	// Assertion 1: exactly ONE new reviews row landed for this booking.
	var reviewsAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM reviews WHERE booking_id = $1`, b.ID,
	).Scan(&reviewsAfter); err != nil {
		t.Fatalf("reviews count: %v", err)
	}
	if reviewsAfter-reviewsBefore != 1 {
		t.Fatalf("reviews rows for booking %s grew by %d, want 1 (the UoW commit must have persisted the Create write)",
			b.ID, reviewsAfter-reviewsBefore)
	}

	// Belt-and-braces: the specific review id we built reloads with the
	// rating intact. Guards against a hypothetical case where SOME review
	// row landed (passing assertion 1) but not the one the service
	// returned.
	reloaded, err := pg.NewReviewRepository(pool).FindByID(ctx, rv.ID)
	if err != nil {
		t.Fatalf("reload review: %v", err)
	}
	if reloaded.Rating != 5 {
		t.Errorf("reloaded review rating = %d, want 5", reloaded.Rating)
	}
	if reloaded.PropertyID != prop.ID {
		t.Errorf("reloaded review property = %s, want %s", reloaded.PropertyID, prop.ID)
	}
	if reloaded.Kind != review.KindGuestToProperty {
		t.Errorf("reloaded review kind = %q, want %q", reloaded.Kind, review.KindGuestToProperty)
	}

	// Assertion 2: exactly ONE new outbox row keyed by event_name
	// 'review.submitted' carries this booking, in pending state. If a
	// future change adds a second event for the same use case this will
	// fail loudly so the test stays honest.
	var pendingForBooking int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'BookingID' = $2
		   AND processed_at IS NULL
		   AND failed_at IS NULL
		   AND dead_lettered_at IS NULL`,
		(event.ReviewSubmitted{}).EventName(), b.ID.String(),
	).Scan(&pendingForBooking); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if pendingForBooking != 1 {
		t.Fatalf("pending ReviewSubmitted rows for booking %s = %d, want 1 (the UoW commit must have appended exactly one ReviewSubmitted record)",
			b.ID, pendingForBooking)
	}

	// Assertion 3: the OVERALL outbox grew by exactly 1. Guards against a
	// future change that adds an extra event but happens to use a
	// different event_name or payload key.
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter-outboxBefore != 1 {
		t.Fatalf("outbox grew by %d after review create, want 1 (the Create UoW currently emits exactly one event — update this test if persistCreate starts emitting more)",
			outboxAfter-outboxBefore)
	}
}

// TestReviewUoW_RollbackOnError_Integration — same setup, but the UoW
// closure returns an error after both writes. After rollback the reviews
// table must hold ZERO rows for this booking (baseline 0) AND no NEW
// outbox rows may carry this booking's id. This is the contract the UoW
// exists to enforce — without it, a partial commit could ship a
// ReviewSubmitted event for a review that was never persisted, or persist
// a review while silently dropping the submission event.
//
// We drive the UoW directly (not through the application service) so the
// closure can simulate the mid-tx failure — the service doesn't expose a
// hook to inject one, and rebuilding it would re-implement the contract
// the test is meant to lock down.
func TestReviewUoW_RollbackOnError_Integration(t *testing.T) {
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

	wantErr := errors.New("simulated mid-tx failure after review + outbox write")
	uow := pg.NewUnitOfWork(pool, nil)
	err = uow.Run(ctx, func(tx port.Tx) error {
		if tx.Reviews == nil {
			t.Fatalf("tx.Reviews is nil — the UoW factory must wire NewReviewTxRepository")
		}
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
		return wantErr // force rollback AFTER all writes
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("uow run err = %v, want %v", err, wantErr)
	}

	// Assertion 1: reviews count for this booking is unchanged from the
	// baseline. The Create INSERT inside the failed UoW must have rolled
	// back with the transaction.
	var reviewsAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM reviews WHERE booking_id = $1`, b.ID,
	).Scan(&reviewsAfter); err != nil {
		t.Fatalf("reviews count: %v", err)
	}
	if reviewsAfter != reviewsBefore {
		t.Fatalf("reviews rows for booking %s after rollback = %d, want %d (Create must have rolled back)",
			b.ID, reviewsAfter, reviewsBefore)
	}

	// Belt-and-braces: the specific review id is not findable. Catches a
	// hypothetical bug where some other review landed but ours did not
	// (or vice versa) — the count check alone could mask that.
	var ourReviewRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM reviews WHERE id = $1`, rv.ID,
	).Scan(&ourReviewRows); err != nil {
		t.Fatalf("count by review id: %v", err)
	}
	if ourReviewRows != 0 {
		t.Fatalf("reviews row for our id %s after rollback = %d, want 0 (the rolled-back Create must leave no trace)",
			rv.ID, ourReviewRows)
	}

	// Assertion 2: NO new outbox row carries this booking id. The
	// Append inside the rolled-back tx must not have left a trace,
	// otherwise the relay would later ship a ReviewSubmitted event for a
	// review that does not exist in the DB (the exact split-brain the
	// UoW exists to prevent — and the bug class S136 closes).
	var leakedForBooking int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'BookingID' = $2`,
		(event.ReviewSubmitted{}).EventName(), b.ID.String(),
	).Scan(&leakedForBooking); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if leakedForBooking != 0 {
		t.Fatalf("ReviewSubmitted rows for booking %s after rollback = %d, want 0 (Append must have rolled back)",
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

// Compile-time guards: this test references the review domain package
// and the ReviewSubmitted event constructor; keep explicit references so
// a future "unused import" reflow doesn't accidentally strip them.
var _ = review.NewPropertyReview
var _ = (event.ReviewSubmitted{}).EventName
