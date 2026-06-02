// Postgres integration tests — opt-in via POSTGRES_TEST_DSN. The
// in-memory UoW harness can prove the application-level contract
// (services produce the right writes + events) but it CANNOT prove
// rollback semantics: in-memory map writes commit immediately, with no
// notion of "rollback if fn errors". The contracts S31 and S35 lean on
// — split write + outbox event commit atomically, fn-error means
// neither persists — live only in the postgres path. These tests
// exercise that path against a real Postgres so the guarantees aren't
// just architectural intent.
//
// Run them locally with:
//
//   POSTGRES_TEST_DSN='postgres://airhost:airhost@localhost:5432/airhost?sslmode=disable' \
//     go test ./internal/infrastructure/persistence/postgres/... -run Integration
//
// In CI: spin up the docker-compose postgres service first (or wire
// testcontainers), then export the same env var.
package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dsnEnv = "POSTGRES_TEST_DSN"

// migrateOnce runs the schema migrations exactly once per test binary —
// individual tests just truncate the tables they touch.
var migrateOnce sync.Once

// withTestDB returns a pool against the env-configured postgres, after
// running migrations and truncating the tables the suite touches. The
// test is skipped when POSTGRES_TEST_DSN is unset, so `go test ./...`
// stays green on dev machines without a database.
func withTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping postgres integration test", dsnEnv)
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

	// migrate once per binary; reads ./migrations from the package dir.
	var migrateErr error
	migrateOnce.Do(func() {
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

	// Clean state for this test: truncate everything the suite writes to,
	// cascading so the FKs (booking → property → user, splits → booking,
	// shares → split) all drop together. Outbox is independent.
	truncate(t, pool,
		"split_payment_shares", "split_payments",
		"bookings", "properties", "users",
		"outbox",
	)
	return pool
}

func truncate(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, tbl := range tables {
		if _, err := pool.Exec(context.Background(),
			"TRUNCATE TABLE "+tbl+" RESTART IDENTITY CASCADE",
		); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
}

// seedHostAndProperty inserts the minimum FK chain (user + property)
// needed by booking tests, returning the property id.
func seedHostAndProperty(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	host, _ := user.NewUser("kc-host-"+uuid.NewString(), "host-"+uuid.NewString()+"@test.dev", "Host", user.RoleHost)
	users := pg.NewUserRepository(pool)
	if err := users.Create(ctx, host); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	addr := property.Address{Line1: "1 Main", City: "Lisbon", Country: "PT"}
	price, _ := shared.NewMoney(10000, "EUR")
	zero, _ := shared.NewMoney(0, "EUR")
	prop, err := property.NewProperty(host.ID, "Test Flat", "desc", property.TypeApartment, addr, price, zero, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	props := pg.NewPropertyRepository(pool)
	if err := props.Create(ctx, prop); err != nil {
		t.Fatalf("seed property: %v", err)
	}
	return prop.ID
}

// newTestBooking returns an unsaved Booking aggregate against a freshly
// seeded property/guest pair.
func newTestBooking(t *testing.T, pool *pgxpool.Pool) (*booking.Booking, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	propID := seedHostAndProperty(t, pool)

	guest, _ := user.NewUser("kc-guest-"+uuid.NewString(), "guest-"+uuid.NewString()+"@test.dev", "Guest", user.RoleGuest)
	users := pg.NewUserRepository(pool)
	if err := users.Create(ctx, guest); err != nil {
		t.Fatalf("seed guest: %v", err)
	}
	dates, err := booking.NewDateRange(time.Now().AddDate(0, 0, 30), time.Now().AddDate(0, 0, 32))
	if err != nil {
		t.Fatalf("dates: %v", err)
	}
	price, _ := shared.NewMoney(10000, "EUR")
	zero, _ := shared.NewMoney(0, "EUR")
	b, err := booking.NewBooking(propID, guest.ID, dates, 2, price, zero, 0.10, booking.Discounts{})
	if err != nil {
		t.Fatalf("new booking: %v", err)
	}
	return b, propID
}

// TestIntegration_UoW_RollbackOnError — when the fn inside uow.Run
// returns an error, neither the booking row NOR the outbox row may
// persist. The in-memory harness cannot test this (no real tx); the
// postgres path is where the contract lives.
func TestIntegration_UoW_RollbackOnError(t *testing.T) {
	pool := withTestDB(t)
	uow := pg.NewUnitOfWork(pool, nil) // relay nil — we don't care about dispatch here
	b, _ := newTestBooking(t, pool)
	wantErr := shared.NewValidationError("simulated mid-tx failure")

	err := uow.Run(context.Background(), func(tx port.Tx) error {
		if err := tx.Bookings.Create(context.Background(), b); err != nil {
			return err
		}
		rec, _ := event.NewRecord(event.BookingRequested{BookingID: b.ID, PropertyTitle: "x"})
		if err := tx.Outbox.Append(context.Background(), rec); err != nil {
			return err
		}
		return wantErr // force rollback
	})
	if err == nil {
		t.Fatalf("expected wantErr, got nil")
	}

	// Assertions: neither table contains the row.
	var n int64
	pool.QueryRow(context.Background(), "SELECT count(*) FROM bookings WHERE id = $1", b.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("booking row persisted after rollback: %d", n)
	}
	pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox").Scan(&n)
	if n != 0 {
		t.Fatalf("outbox row persisted after rollback: %d", n)
	}
}

// TestIntegration_UoW_CommitAtomic — the happy path: fn returns nil,
// both rows land. Locks the inverse of the rollback contract.
func TestIntegration_UoW_CommitAtomic(t *testing.T) {
	pool := withTestDB(t)
	uow := pg.NewUnitOfWork(pool, nil)
	b, _ := newTestBooking(t, pool)

	err := uow.Run(context.Background(), func(tx port.Tx) error {
		if err := tx.Bookings.Create(context.Background(), b); err != nil {
			return err
		}
		rec, _ := event.NewRecord(event.BookingRequested{BookingID: b.ID, PropertyTitle: "x"})
		return tx.Outbox.Append(context.Background(), rec)
	})
	if err != nil {
		t.Fatalf("uow.Run: %v", err)
	}

	var n int64
	pool.QueryRow(context.Background(), "SELECT count(*) FROM bookings WHERE id = $1", b.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("booking row count = %d, want 1", n)
	}
	pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox").Scan(&n)
	if n != 1 {
		t.Fatalf("outbox row count = %d, want 1", n)
	}
}

// TestIntegration_SplitPayment_TxRepository_AtomicWithOutbox — proves the
// S31/S35 path: split parent row, both share rows, AND the outbox event
// commit atomically. If any one of these failed mid-tx none would land.
func TestIntegration_SplitPayment_TxRepository_AtomicWithOutbox(t *testing.T) {
	pool := withTestDB(t)
	uow := pg.NewUnitOfWork(pool, nil)
	b, _ := newTestBooking(t, pool)

	// Build a 2-share split that satisfies the aggregate invariants.
	sp, err := splitpayment.New(b.ID, b.GuestID, "organizer@test.dev", "EUR", 10000, []splitpayment.ShareInput{
		{Email: "organizer@test.dev", AmountCents: 4000},
		{Email: "friend@test.dev", AmountCents: 6000},
	})
	if err != nil {
		t.Fatalf("new split: %v", err)
	}

	err = uow.Run(context.Background(), func(tx port.Tx) error {
		if err := tx.Bookings.Create(context.Background(), b); err != nil {
			return err
		}
		if err := tx.SplitPayments.Create(context.Background(), sp); err != nil {
			return err
		}
		rec, _ := event.NewRecord(event.BookingRequested{BookingID: b.ID, PropertyTitle: "x"})
		return tx.Outbox.Append(context.Background(), rec)
	})
	if err != nil {
		t.Fatalf("uow.Run: %v", err)
	}

	var parents, shares, outboxRows int64
	pool.QueryRow(context.Background(), "SELECT count(*) FROM split_payments WHERE id = $1", sp.ID).Scan(&parents)
	pool.QueryRow(context.Background(), "SELECT count(*) FROM split_payment_shares WHERE split_payment_id = $1", sp.ID).Scan(&shares)
	pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox").Scan(&outboxRows)

	if parents != 1 || shares != 2 || outboxRows != 1 {
		t.Fatalf("post-commit row counts = parents=%d shares=%d outbox=%d, want 1/2/1", parents, shares, outboxRows)
	}
}

// TestIntegration_SplitPayment_PoolPath_AtomicParentAndShares — the
// non-UoW path (direct pool-bound repo) still wraps parent+shares in an
// internal transaction. Important: this is what bookingapp used pre-S35
// and what any future caller without a UoW will use.
func TestIntegration_SplitPayment_PoolPath_AtomicParentAndShares(t *testing.T) {
	pool := withTestDB(t)
	b, _ := newTestBooking(t, pool)
	// seed the booking via UoW first so the FK is satisfied
	if err := pg.NewUnitOfWork(pool, nil).Run(context.Background(), func(tx port.Tx) error {
		return tx.Bookings.Create(context.Background(), b)
	}); err != nil {
		t.Fatalf("seed booking: %v", err)
	}

	sp, err := splitpayment.New(b.ID, b.GuestID, "organizer@test.dev", "EUR", 10000, []splitpayment.ShareInput{
		{Email: "organizer@test.dev", AmountCents: 5000},
		{Email: "friend@test.dev", AmountCents: 5000},
	})
	if err != nil {
		t.Fatalf("new split: %v", err)
	}

	repo := pg.NewSplitPaymentRepository(pool) // pool-bound, opens its own tx
	if err := repo.Create(context.Background(), sp); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var parents, shares int64
	pool.QueryRow(context.Background(), "SELECT count(*) FROM split_payments WHERE id = $1", sp.ID).Scan(&parents)
	pool.QueryRow(context.Background(), "SELECT count(*) FROM split_payment_shares WHERE split_payment_id = $1", sp.ID).Scan(&shares)
	if parents != 1 || shares != 2 {
		t.Fatalf("pool-path row counts = parents=%d shares=%d, want 1/2", parents, shares)
	}
}
