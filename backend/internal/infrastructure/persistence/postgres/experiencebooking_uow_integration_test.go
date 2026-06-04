package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/shared"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
)

// S115 — Postgres integration tests for the ExperienceBooking Create flow
// inside a UnitOfWork (S87 / WF-GAP-020). The application / memory tests
// already prove that the experiencebooking service routes Create / Confirm
// / Cancel / Complete through the UoW and appends the matching lifecycle
// event; this file closes the gap by exercising the actual pgx.Tx-bound
// ExperienceBookingRepository so the experience_bookings INSERT and the
// outbox INSERT really do commit (or roll back) together against a live
// Postgres.
//
// Each test:
//   - skips cleanly when POSTGRES_TEST_DSN is unset (the suite stays green
//     for dev machines without a database)
//   - reuses the withTestDB / truncate / seed helpers from integration_test.go
//     (newTestExperienceBooking + seedExperienceForBookings live alongside
//     in experience_booking_integration_test.go)
//   - rolls its own UnitOfWork against the live pool so the closure sees a
//     pgx.Tx-bound ExperienceBookingRepository the same way the
//     experiencebooking service does post-S87

// TestExperienceBookingUoW_CreateWritesAtomicallyWithOutbox_Integration —
// the happy path. Inside a UoW we (a) Save a fresh ExperienceBooking via
// tx.ExperienceBookings.Create AND (b) append an ExperienceBookingCreated
// record to tx.Outbox. After commit, the experience_bookings row must be
// present AND the outbox must hold the event in pending state
// (processed_at, failed_at, dead_lettered_at all NULL). This is the
// in-tx contract S87 exists to enforce — a payment-subscriber crash
// mid-publish can no longer leave a Created event unrecorded against a
// booking that is already persisted (or vice-versa).
func TestExperienceBookingUoW_CreateWritesAtomicallyWithOutbox_Integration(t *testing.T) {
	pool := withTestDB(t)
	// experience_bookings and experiences aren't in the global truncate
	// list (withTestDB doesn't know about every BC); clear them here so
	// repeated runs against a stateful local Postgres stay green.
	truncate(t, pool, "experience_bookings", "experiences")
	ctx := context.Background()

	// Seed a parent experience (host + experience row). The booking
	// aggregate carries explicit experienceID/hostID; the FK on
	// experience_bookings(experience_id) requires the experience row to
	// exist before the UoW Create runs.
	b := newTestExperienceBooking(t, pool, 48*time.Hour, 2)

	// Run the UoW: persist the booking + append the Created event. This
	// mirrors what experiencebooking.Service.Create does after S87 — the
	// in-tx path is the whole point of the slice (write + event commit
	// atomically).
	uow := pg.NewUnitOfWork(pool, nil)
	if err := uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.ExperienceBookings.Create(ctx, b); err != nil {
			return err
		}
		rec, err := event.NewRecord(experiencebooking.ExperienceBookingCreated{
			BookingID:       b.ID,
			ExperienceID:    b.ExperienceID,
			ExperienceTitle: "Booking test exp", // matches seedExperienceForBookings
			HostID:          b.HostID,
			GuestID:         b.GuestID,
			TotalCents:      b.Pricing.Total.AmountCents(),
			Currency:        b.Pricing.Total.Currency(),
			OccurredAt:      b.CreatedAt,
		})
		if err != nil {
			return err
		}
		return tx.Outbox.Append(ctx, rec)
	}); err != nil {
		t.Fatalf("uow run: %v", err)
	}

	// Assertion 1: the booking row landed and round-trips correctly.
	reloaded, err := pg.NewExperienceBookingRepository(pool).FindByID(ctx, b.ID)
	if err != nil {
		t.Fatalf("reload experience booking: %v", err)
	}
	if reloaded.ID != b.ID {
		t.Errorf("reloaded booking id = %s, want %s", reloaded.ID, b.ID)
	}
	if reloaded.Status != experiencebooking.StatusPending {
		t.Errorf("status after commit = %q, want %q", reloaded.Status, experiencebooking.StatusPending)
	}

	// Assertion 2: the outbox has exactly one ExperienceBookingCreated
	// row tied to this booking, still in pending state (none of the
	// terminal column timestamps populated). The check uses the BookingID
	// extracted from the JSONB payload so it survives other suite tests
	// running in parallel against the same database.
	var pending int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'BookingID' = $2
		   AND processed_at IS NULL
		   AND failed_at IS NULL
		   AND dead_lettered_at IS NULL`,
		(experiencebooking.ExperienceBookingCreated{}).EventName(), b.ID.String(),
	).Scan(&pending); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending outbox rows for experience booking %s = %d, want 1 (the UoW commit must have appended exactly one ExperienceBookingCreated record)", b.ID, pending)
	}
}

// TestExperienceBookingUoW_RollbackOnError_Integration — same setup, but
// the UoW closure returns an error after both writes. After rollback,
// NO experience_bookings row may exist for the booking id AND NO outbox
// row may carry that booking_id (the Append rolled back with the
// transaction). This is the contract S87 exists to enforce — without
// the UoW, a partial commit could ship an ExperienceBookingCreated
// event for a booking that was never persisted (or persist a booking
// whose Created event the relay would never see).
func TestExperienceBookingUoW_RollbackOnError_Integration(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "experience_bookings", "experiences")
	ctx := context.Background()

	b := newTestExperienceBooking(t, pool, 48*time.Hour, 2)

	wantErr := errors.New("simulated failure after experience booking create")
	uow := pg.NewUnitOfWork(pool, nil)
	err := uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.ExperienceBookings.Create(ctx, b); err != nil {
			return err
		}
		rec, err := event.NewRecord(experiencebooking.ExperienceBookingCreated{
			BookingID:       b.ID,
			ExperienceID:    b.ExperienceID,
			ExperienceTitle: "Booking test exp",
			HostID:          b.HostID,
			GuestID:         b.GuestID,
			TotalCents:      b.Pricing.Total.AmountCents(),
			Currency:        b.Pricing.Total.Currency(),
			OccurredAt:      b.CreatedAt,
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

	// Assertion 1: the experience_bookings row does NOT exist. The
	// in-tx INSERT must have rolled back with the transaction; reading
	// the booking back via the repo should surface shared.ErrNotFound.
	_, err = pg.NewExperienceBookingRepository(pool).FindByID(ctx, b.ID)
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("FindByID after rollback err = %v, want shared.ErrNotFound (the Create must have rolled back)", err)
	}

	// Belt-and-braces: also check the row count directly, so a future
	// change to the repo's not-found mapping can't accidentally mask a
	// regression where the row survives rollback.
	var bookingRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM experience_bookings WHERE id = $1`, b.ID,
	).Scan(&bookingRows); err != nil {
		t.Fatalf("experience_bookings count: %v", err)
	}
	if bookingRows != 0 {
		t.Fatalf("experience_bookings row for %s after rollback = %d, want 0", b.ID, bookingRows)
	}

	// Assertion 2: NO outbox row carries this booking's id. The Append
	// inside the rolled-back tx must not have left a trace, otherwise
	// the relay would later ship an ExperienceBookingCreated event for
	// a booking the DB never persisted (the exact split-brain S87 fixes).
	var outboxRows int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'BookingID' = $2`,
		(experiencebooking.ExperienceBookingCreated{}).EventName(), b.ID.String(),
	).Scan(&outboxRows); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if outboxRows != 0 {
		t.Fatalf("outbox rows for experience booking %s after rollback = %d, want 0 (Append must have rolled back)", b.ID, outboxRows)
	}
}

// Compile-time guard: the test file references the experiencebooking
// domain package and the event constructor; keep an explicit reference so
// a future "unused import" reflow doesn't accidentally strip them.
var _ = experiencebooking.StatusPending
var _ = (experiencebooking.ExperienceBookingCreated{}).EventName
var _ uuid.UUID
