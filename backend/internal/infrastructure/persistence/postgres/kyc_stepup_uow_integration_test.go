package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// S143 — Postgres integration tests for the KYCStepUpRequired outbox
// durability path (S140 — WF-GAP-019). The application-layer tests in
// backend/internal/application/booking/service_test.go already prove that
// bookingapp.Service.enforceStepUp emits the event via the in-memory UoW
// when the high-value gate trips, and that the TTL deduper collapses
// retries inside its quiet window. This file closes the gap by exercising
// the actual pgx.Tx-bound outbox so the kyc.step_up_required row really
// does land on a live Postgres — and, just as importantly, that no
// bookings row leaks alongside it (the gate denies BEFORE the booking
// INSERT).
//
// Each test:
//   - skips cleanly when POSTGRES_TEST_DSN is unset (the suite stays green
//     for dev machines without a database)
//   - reuses the withTestDB / truncate / seedHostAndProperty helpers from
//     integration_test.go (which already truncates bookings, properties,
//     users, splits, and outbox between tests)
//   - drives bookingapp.Service.Create end-to-end so the test exercises
//     the production code path through enforceStepUp — including the
//     uow.Run that appends the outbox record before returning the gate
//     error to the caller
//
// Event-count rationale: investigation of bookingapp.Service.enforceStepUp
// (service.go) shows the gate emits EXACTLY ONE event —
// KYCStepUpRequired — and performs ZERO bookings writes (the gate denies
// before the booking INSERT). So the atomicity contract this file
// protects is: one kyc.step_up_required outbox row, zero bookings rows.

// stepUpStubVerifier is the local IdentityVerifier stub for these tests.
// It always reports "not verified" so the high-value gate trips
// deterministically on every Create. Named with the "stepUp" prefix so it
// doesn't collide with any verifier helper a sibling integration_test.go
// might add in the future.
type stepUpStubVerifier struct{}

func (stepUpStubVerifier) IsVerified(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

// seedPublishedPropertyAndGuest lands the FK chain bookingapp.Service.Create
// needs to reach enforceStepUp: a host user, a PUBLISHED property (with a
// photo, since Publish() refuses a zero-photo listing), and an unverified
// guest user. seedHostAndProperty (in integration_test.go) creates the
// property in its default StatusDraft and never publishes it; we publish
// here so CanBeBookedBy lets the Create flow proceed all the way down to
// the step-up gate.
//
// Returns the published property and the guest's id. The host id is
// unused by the step-up path (the event payload only carries guest +
// listing), so it is intentionally not surfaced.
func seedPublishedPropertyAndGuest(t *testing.T, pool *pgxpool.Pool) (*property.Property, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	host, _ := user.NewUser("kc-host-"+uuid.NewString(), "host-"+uuid.NewString()+"@test.dev", "Host", user.RoleHost)
	users := pg.NewUserRepository(pool)
	if err := users.Create(ctx, host); err != nil {
		t.Fatalf("seed host: %v", err)
	}

	addr := property.Address{Line1: "1 Main", City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	price, _ := shared.NewMoney(10000, "EUR") // 100.00/night
	zero, _ := shared.NewMoney(0, "EUR")
	prop, err := property.NewProperty(host.ID, "Sunny flat", "desc", property.TypeApartment, addr, price, zero, 4, 2, 2, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	// Publish() requires at least one photo — add a stub and flip the
	// status so CanBeBookedBy lets the booking service reach the
	// step-up gate.
	prop.AddPhoto("k", "http://x/k.jpg")
	if err := prop.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := pg.NewPropertyRepository(pool).Create(ctx, prop); err != nil {
		t.Fatalf("seed property: %v", err)
	}

	guest, _ := user.NewUser("kc-guest-"+uuid.NewString(), "guest-"+uuid.NewString()+"@test.dev", "Guest", user.RoleGuest)
	if err := users.Create(ctx, guest); err != nil {
		t.Fatalf("seed guest: %v", err)
	}
	return prop, guest.ID
}

// stepUpService wires a bookingapp.Service against the live pool, with
// the high-value gate enabled (EUR threshold 10000 — 100.00, below the
// 20000-cent total a 2-night stay at the seeded listing will produce)
// and the verifier hard-wired to "not verified". ttl is forwarded to
// WithStepUpEventTTL — pass 0 to disable the deduper, > 0 to enable it.
func stepUpService(pool *pgxpool.Pool, ttl time.Duration) *bookingapp.Service {
	uow := pg.NewUnitOfWork(pool, nil) // relay nil — we read the outbox table directly
	svc := bookingapp.NewService(
		pg.NewBookingRepository(pool),
		pg.NewPropertyRepository(pool),
		pg.NewBlockRepository(pool),
		pg.NewCouponRepository(pool),
		pg.NewPriceRuleRepository(pool),
		0, // serviceFeeRate — 0 keeps the total math predictable (2 nights * 10000 = 20000)
		stepUpStubVerifier{},
		false, // requireKYC OFF — only the high-value gate should fire
		uow,
	).WithHighValueThresholds(map[string]int64{"EUR": 10000})
	if ttl > 0 {
		svc = svc.WithStepUpEventTTL(ttl)
	} else {
		// Explicit "disable dedupe" — every gate-trip emits an event. The
		// gate test uses this path so its single Create call produces a
		// single row even if a (hypothetical) later refactor moved
		// dedupe's default off-state.
		svc = svc.WithStepUpEventTTL(0)
	}
	return svc
}

// TestKYCStepUpUoW_GateEmitsOutboxRow_Integration — the happy path for
// the step-up emission contract. We drive bookingapp.Service.Create with
// an unverified guest on a published EUR-priced listing whose 2-night
// total (20000 cents) clears the 10000-cent threshold. The Create call
// must:
//   - return *shared.KYCStepUpError (the gate denies the booking)
//   - leave the bookings table empty (no row INSERTed before the gate)
//   - append exactly one outbox row keyed by event_name
//     "kyc.step_up_required", with a payload that carries the guest,
//     listing, threshold and currency
func TestKYCStepUpUoW_GateEmitsOutboxRow_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	prop, guestID := seedPublishedPropertyAndGuest(t, pool)

	// Baselines captured BEFORE the use case runs. withTestDB truncates
	// everything, so these are 0 in practice — pinning them keeps the
	// test resilient against any future helper that seeds rows during
	// setup.
	var outboxBefore, bookingsBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM bookings`).Scan(&bookingsBefore); err != nil {
		t.Fatalf("baseline bookings count: %v", err)
	}

	// Drive the service end-to-end — ttl=0 (dedupe off) so a single
	// Create produces a single row, no surprises from the deduper.
	svc := stepUpService(pool, 0)

	// 2-night stay at 10000/night = 20000-cent total, clearing the
	// 10000-cent EUR threshold. The guest is hard-wired unverified by
	// stepUpStubVerifier, so the gate denies.
	_, err := svc.Create(ctx, bookingapp.CreateInput{
		GuestID:    guestID,
		PropertyID: prop.ID,
		CheckIn:    time.Now().UTC().AddDate(0, 0, 30),
		CheckOut:   time.Now().UTC().AddDate(0, 0, 32),
		Guests:     2,
	})
	if err == nil {
		t.Fatal("expected the high-value gate to deny the booking, got nil error")
	}
	var stepUp *shared.KYCStepUpError
	if !errors.As(err, &stepUp) {
		t.Fatalf("expected *shared.KYCStepUpError, got %T (%v)", err, err)
	}
	if stepUp.ThresholdCents != 10000 || stepUp.Currency != "EUR" {
		t.Errorf("step-up error = {%d %s}, want {10000 EUR}", stepUp.ThresholdCents, stepUp.Currency)
	}

	// Assertion 1: the bookings table is unchanged from the baseline.
	// enforceStepUp denies BEFORE the booking INSERT, so no row may have
	// landed for this guest. This is the half of the contract the memory
	// tests can't exercise (the in-memory UoW commits writes immediately
	// even when the use case returns the gate error after appending an
	// outbox row; the postgres path proves the gate actually short-
	// circuits before the bookings INSERT).
	var bookingsAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM bookings`).Scan(&bookingsAfter); err != nil {
		t.Fatalf("bookings count: %v", err)
	}
	if bookingsAfter != bookingsBefore {
		t.Fatalf("bookings rows after gate-deny = %d, want %d (the gate must short-circuit before any booking INSERT)",
			bookingsAfter, bookingsBefore)
	}

	// Assertion 2: exactly ONE new outbox row keyed by event_name
	// "kyc.step_up_required" carries this guest + listing, in pending
	// state. If a future change adds a second event for the same gate
	// trip this will fail loudly so the test stays honest.
	var pending int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'GuestID' = $2
		   AND processed_at IS NULL
		   AND failed_at IS NULL
		   AND dead_lettered_at IS NULL`,
		(event.KYCStepUpRequired{}).EventName(), guestID.String(),
	).Scan(&pending); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending KYCStepUpRequired rows for guest %s = %d, want 1 (the UoW commit must have appended exactly one record)",
			guestID, pending)
	}

	// Assertion 3: the OVERALL outbox grew by exactly 1. Guards against
	// a future change that adds an extra event but happens to use a
	// different event_name or payload key (so assertion 2 would still
	// see 1).
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter-outboxBefore != 1 {
		t.Fatalf("outbox grew by %d after step-up trip, want 1 (the gate currently emits exactly one event — update this test if enforceStepUp starts emitting more)",
			outboxAfter-outboxBefore)
	}

	// Assertion 4: the payload field shape matches what the subscribers
	// (notification + email BCs) read. We decode into KYCStepUpRequired
	// to round-trip through the same encoder events.Register uses, so a
	// future field rename surfaces here.
	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT payload FROM outbox
		 WHERE event_name = $1
		   AND payload->>'GuestID' = $2`,
		(event.KYCStepUpRequired{}).EventName(), guestID.String(),
	).Scan(&payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	var decoded event.KYCStepUpRequired
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded.GuestID != guestID {
		t.Errorf("payload.GuestID = %s, want %s", decoded.GuestID, guestID)
	}
	if decoded.PropertyID != prop.ID {
		t.Errorf("payload.PropertyID = %s, want %s", decoded.PropertyID, prop.ID)
	}
	if decoded.PropertyTitle != prop.Title {
		t.Errorf("payload.PropertyTitle = %q, want %q", decoded.PropertyTitle, prop.Title)
	}
	if decoded.ThresholdCents != 10000 {
		t.Errorf("payload.ThresholdCents = %d, want 10000", decoded.ThresholdCents)
	}
	if decoded.Currency != "EUR" {
		t.Errorf("payload.Currency = %q, want EUR", decoded.Currency)
	}
	// The total math is deterministic: 2 nights * 10000 + 0 cleaning + 0%
	// service fee = 20000. Pinning it explicitly catches a future pricing
	// change that drifts the total but leaves the threshold lookup
	// unchanged.
	if decoded.TotalCents != 20000 {
		t.Errorf("payload.TotalCents = %d, want 20000 (2 nights * 10000 with zero fees)", decoded.TotalCents)
	}
}

// TestKYCStepUpUoW_DedupeCollapsesRetries_Integration — drives Create
// three times for the same (guest, listing, currency) tuple inside the
// step-up TTL window. The first attempt emits an event; the next two
// trip the gate but stay silent for downstream subscribers — the
// deduper must collapse them at the postgres outbox layer too, not just
// in memory. After all three attempts:
//   - exactly ONE outbox row keyed by "kyc.step_up_required" exists for
//     this guest (the deduper collapsed retries #2 and #3)
//   - zero bookings rows (every attempt was denied by the gate)
//
// This is the postgres mirror of TestCreate_StepUpDedupesEmissionsWithinTTL
// in backend/internal/application/booking/service_test.go — the memory
// version proves the in-process deduper; this version proves the same
// guarantee survives a real outbox INSERT path.
func TestKYCStepUpUoW_DedupeCollapsesRetries_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	prop, guestID := seedPublishedPropertyAndGuest(t, pool)

	var outboxBefore, bookingsBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM bookings`).Scan(&bookingsBefore); err != nil {
		t.Fatalf("baseline bookings count: %v", err)
	}

	// One-hour TTL is wide enough that all three retries land inside
	// the same quiet window — the deduper must coalesce them.
	svc := stepUpService(pool, time.Hour)

	checkIn := time.Now().UTC().AddDate(0, 0, 30)
	checkOut := time.Now().UTC().AddDate(0, 0, 32)
	for i := 0; i < 3; i++ {
		_, err := svc.Create(ctx, bookingapp.CreateInput{
			GuestID:    guestID,
			PropertyID: prop.ID,
			CheckIn:    checkIn,
			CheckOut:   checkOut,
			Guests:     2,
		})
		if err == nil {
			t.Fatalf("attempt %d: expected the gate to deny the booking, got nil", i)
		}
		var stepUp *shared.KYCStepUpError
		if !errors.As(err, &stepUp) {
			t.Fatalf("attempt %d: expected *shared.KYCStepUpError, got %T (%v)", i, err, err)
		}
	}

	// Assertion 1: the bookings table is still empty. Every Create
	// returned the gate error, so no row could have landed.
	var bookingsAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM bookings`).Scan(&bookingsAfter); err != nil {
		t.Fatalf("bookings count: %v", err)
	}
	if bookingsAfter != bookingsBefore {
		t.Fatalf("bookings rows after 3 gate-denies = %d, want %d", bookingsAfter, bookingsBefore)
	}

	// Assertion 2: exactly ONE step-up outbox row for this guest — the
	// deduper collapsed retries #2 and #3. This is the postgres
	// counterpart to the memory-level dedupe test.
	var rowsForGuest int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'GuestID' = $2`,
		(event.KYCStepUpRequired{}).EventName(), guestID.String(),
	).Scan(&rowsForGuest); err != nil {
		t.Fatalf("outbox count by guest: %v", err)
	}
	if rowsForGuest != 1 {
		t.Fatalf("KYCStepUpRequired rows for guest %s after 3 retries = %d, want 1 (the in-memory TTL deduper must have collapsed retries #2 and #3 at the outbox layer too)",
			guestID, rowsForGuest)
	}

	// Assertion 3: the OVERALL outbox grew by exactly 1 across all
	// three Create calls. Belt-and-braces against a future change that
	// emits a different event under a different key — the dedupe
	// contract is about TOTAL outbox row growth, not just the
	// per-guest count.
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter-outboxBefore != 1 {
		t.Fatalf("outbox grew by %d after 3 gate-denies, want 1 (the deduper collapses retries inside the TTL window)",
			outboxAfter-outboxBefore)
	}
}

// Compile-time guards: this test references the KYCStepUpRequired
// constructor and the booking application package; keep explicit
// references so a future "unused import" reflow doesn't accidentally
// strip them.
var _ = (event.KYCStepUpRequired{}).EventName
var _ = bookingapp.NewService
