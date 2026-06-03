// Postgres integration tests for experiencebooking.Repository (S81).
// Mirrors the env-skip pattern from S41/S55/S72/S75 — opt in via
// POSTGRES_TEST_DSN, run from CI's postgres-integration job.
//
// The in-memory adapter gives the application-level contract; this
// file covers what the in-memory path cannot:
//
//   - Pricing JSONB round-trips faithfully — per-guest price, subtotal,
//     service fee, total + currency all survive the encoding/json pgx
//     hop and rebuild as proper shared.Money values.
//   - ListByGuest / ListByHost order match the memory adapter:
//     created_at DESC, id ASC tiebreaker, so swapping backends doesn't
//     reshuffle paged views.
//   - FindOverlapping is enforced in SQL: cancelled bookings are
//     excluded, the half-open semantics hold (touching but not
//     overlapping windows return zero hits), and bookings against a
//     different experience are ignored.
//
// Run locally with:
//
//	POSTGRES_TEST_DSN='postgres://airhost:airhost@localhost:5432/airhost?sslmode=disable' \
//	  go test ./internal/infrastructure/persistence/postgres/... -run TestIntegration_ExperienceBooking
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/experience"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/shared"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedExperienceForBookings inserts a parent experience row so the
// experience_bookings FK is satisfied. Returns the experience id and
// host id for use in the booking aggregate.
func seedExperienceForBookings(t *testing.T, pool *pgxpool.Pool) (expID, hostID uuid.UUID) {
	t.Helper()
	hostID = uuid.New()
	price, err := shared.NewMoney(7500, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	addr := experience.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	e, err := experience.NewExperience(hostID, "Booking test exp", "desc",
		experience.CategoryTour, addr, 120, 8, price, "en")
	if err != nil {
		t.Fatalf("NewExperience: %v", err)
	}
	if err := pg.NewExperienceRepository(pool).Create(context.Background(), e); err != nil {
		t.Fatalf("seed experience: %v", err)
	}
	return e.ID, hostID
}

// newTestExperienceBooking builds a Booking aggregate against a
// freshly-seeded experience. startOffset is added to "now" so callers
// can stagger session times for overlap / ordering tests.
func newTestExperienceBooking(t *testing.T, pool *pgxpool.Pool, startOffset time.Duration, guests int) *experiencebooking.Booking {
	t.Helper()
	expID, hostID := seedExperienceForBookings(t, pool)
	startAt := time.Now().UTC().Add(startOffset)
	sess, err := experiencebooking.NewSession(startAt, 120, 30, 1440)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	price, _ := shared.NewMoney(7500, "EUR")
	b, err := experiencebooking.NewBooking(expID, hostID, uuid.New(), sess, guests, 8, price, 0.10)
	if err != nil {
		t.Fatalf("NewBooking: %v", err)
	}
	return b
}

// TestIntegration_ExperienceBooking_CreateFindRoundTrip — every domain
// field, including the JSONB Pricing breakdown, must survive a Create +
// FindByID hop. A regression here would corrupt receipts and payouts
// the moment the postgres adapter is wired in.
func TestIntegration_ExperienceBooking_CreateFindRoundTrip(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "experience_bookings", "experiences")
	repo := pg.NewExperienceBookingRepository(pool)
	ctx := context.Background()

	b := newTestExperienceBooking(t, pool, 48*time.Hour, 3)
	if err := repo.Create(ctx, b); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, b.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID != b.ID || got.ExperienceID != b.ExperienceID || got.HostID != b.HostID || got.GuestID != b.GuestID {
		t.Errorf("ids lost: got=%+v want=%+v", got, b)
	}
	if got.Guests != 3 {
		t.Errorf("guests = %d, want 3", got.Guests)
	}
	if got.Session.DurationMinutes != 120 {
		t.Errorf("duration = %d, want 120", got.Session.DurationMinutes)
	}
	if !got.Session.StartAt.Equal(b.Session.StartAt) {
		t.Errorf("session start = %v, want %v", got.Session.StartAt, b.Session.StartAt)
	}
	if got.Status != experiencebooking.StatusPending {
		t.Errorf("status = %s, want pending", got.Status)
	}
	// Pricing JSONB round-trip — every Money field intact.
	if got.Pricing.PricePerGuest.AmountCents() != 7500 || got.Pricing.PricePerGuest.Currency() != "EUR" {
		t.Errorf("pricePerGuest lost: %s", got.Pricing.PricePerGuest)
	}
	if got.Pricing.Guests != 3 {
		t.Errorf("pricing.guests = %d, want 3", got.Pricing.Guests)
	}
	if got.Pricing.Subtotal.AmountCents() != 22500 { // 7500 * 3
		t.Errorf("subtotal = %s, want 225.00 EUR", got.Pricing.Subtotal)
	}
	if got.Pricing.ServiceFee.AmountCents() != 2250 { // 22500 * 0.10
		t.Errorf("serviceFee = %s, want 22.50 EUR", got.Pricing.ServiceFee)
	}
	if got.Pricing.Total.AmountCents() != 24750 { // 22500 + 2250
		t.Errorf("total = %s, want 247.50 EUR", got.Pricing.Total)
	}
	if got.Pricing.Total.Currency() != "EUR" {
		t.Errorf("currency lost: %s", got.Pricing.Total.Currency())
	}
}

// TestIntegration_ExperienceBooking_ListByGuest_PaginationOrdering
// seeds three bookings for one guest (with staggered CreatedAt) plus
// one booking for an unrelated guest, then walks the pages and asserts
// strict newest-first ordering with the unrelated row never leaking.
func TestIntegration_ExperienceBooking_ListByGuest_PaginationOrdering(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "experience_bookings", "experiences")
	repo := pg.NewExperienceBookingRepository(pool)
	ctx := context.Background()

	guestID := uuid.New()
	base := time.Now().UTC().Add(-3 * time.Hour)

	// Three bookings for the same guest at staggered times. Inserted in
	// deliberately scrambled order so the test passes only when the
	// SQL ORDER BY actually runs.
	mk := func(createdAt time.Time) *experiencebooking.Booking {
		b := newTestExperienceBooking(t, pool, 48*time.Hour, 2)
		b.GuestID = guestID
		b.CreatedAt = createdAt
		b.UpdatedAt = createdAt
		return b
	}
	oldest := mk(base)
	middle := mk(base.Add(1 * time.Hour))
	newest := mk(base.Add(2 * time.Hour))

	// One unrelated booking — different guest — must never appear in
	// the result set.
	other := newTestExperienceBooking(t, pool, 72*time.Hour, 2)

	for _, b := range []*experiencebooking.Booking{middle, oldest, newest, other} {
		if err := repo.Create(ctx, b); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	res, err := repo.ListByGuest(ctx, guestID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("ListByGuest: %v", err)
	}
	if res.Total != 3 || len(res.Items) != 3 {
		t.Fatalf("counts = %d/%d, want 3/3 (other guest leaked?)", res.Total, len(res.Items))
	}
	if res.Items[0].ID != newest.ID || res.Items[1].ID != middle.ID || res.Items[2].ID != oldest.ID {
		var order []string
		for _, it := range res.Items {
			order = append(order, it.CreatedAt.Format(time.RFC3339))
		}
		t.Errorf("order = %v, want [newest middle oldest]", order)
	}

	// Page 2 of 2 — limit 2, offset 2: only oldest remains, and the
	// total still reports all 3 (so the caller can know there are more
	// rows even though this page returned 1).
	page2, err := repo.ListByGuest(ctx, guestID, shared.NewPage(2, 2))
	if err != nil {
		t.Fatalf("ListByGuest page2: %v", err)
	}
	if page2.Total != 3 {
		t.Errorf("page2 total = %d, want 3", page2.Total)
	}
	if len(page2.Items) != 1 || page2.Items[0].ID != oldest.ID {
		t.Errorf("page2 items = %d (first = %v), want 1 (oldest)", len(page2.Items), page2.Items)
	}
}

// TestIntegration_ExperienceBooking_ListByHost mirrors the guest test
// but filters by host_id. Confirms the host dashboard's "bookings
// against my experiences" view is sourced from the right index.
func TestIntegration_ExperienceBooking_ListByHost(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "experience_bookings", "experiences")
	repo := pg.NewExperienceBookingRepository(pool)
	ctx := context.Background()

	// Two bookings against the same host. We re-seed the host id on
	// each booking so the FK to experiences still points at distinct
	// rows but the host_id matches.
	hostID := uuid.New()
	expA, _ := seedExperienceForBookings(t, pool)
	expB, _ := seedExperienceForBookings(t, pool)

	mk := func(expID uuid.UUID, offset time.Duration) *experiencebooking.Booking {
		startAt := time.Now().UTC().Add(48*time.Hour + offset)
		sess, _ := experiencebooking.NewSession(startAt, 120, 30, 1440)
		price, _ := shared.NewMoney(7500, "EUR")
		b, err := experiencebooking.NewBooking(expID, hostID, uuid.New(), sess, 2, 8, price, 0.10)
		if err != nil {
			t.Fatalf("NewBooking: %v", err)
		}
		return b
	}
	first := mk(expA, 0)
	first.CreatedAt = time.Now().UTC().Add(-2 * time.Hour)
	first.UpdatedAt = first.CreatedAt
	second := mk(expB, 1*time.Hour)
	second.CreatedAt = time.Now().UTC().Add(-1 * time.Hour)
	second.UpdatedAt = second.CreatedAt

	for _, b := range []*experiencebooking.Booking{first, second} {
		if err := repo.Create(ctx, b); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	// A booking against a third experience with a different host must
	// not leak into the result set.
	stranger := newTestExperienceBooking(t, pool, 96*time.Hour, 2)
	if err := repo.Create(ctx, stranger); err != nil {
		t.Fatalf("Create stranger: %v", err)
	}

	res, err := repo.ListByHost(ctx, hostID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("ListByHost: %v", err)
	}
	if res.Total != 2 || len(res.Items) != 2 {
		t.Fatalf("counts = %d/%d, want 2/2 (stranger leaked?)", res.Total, len(res.Items))
	}
	// Newest-first ordering — second has the more recent CreatedAt.
	if res.Items[0].ID != second.ID || res.Items[1].ID != first.ID {
		t.Errorf("order wrong: [0]=%s [1]=%s", res.Items[0].ID, res.Items[1].ID)
	}
}

// TestIntegration_ExperienceBooking_FindOverlapping exercises the
// half-open overlap predicate AND the "cancelled is excluded" carve-out.
// Seeds an active booking + a cancelled booking at the same start, then
// queries with windows that touch the boundary and across it.
func TestIntegration_ExperienceBooking_FindOverlapping(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "experience_bookings", "experiences")
	repo := pg.NewExperienceBookingRepository(pool)
	ctx := context.Background()

	expID, hostID := seedExperienceForBookings(t, pool)
	// Fix a reference start so the overlap windows can be expressed
	// relative to it without time-of-day flakes.
	base := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Hour)
	mkBooking := func(start time.Time) *experiencebooking.Booking {
		sess, err := experiencebooking.NewSession(start, 120, 30, 1440)
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		price, _ := shared.NewMoney(7500, "EUR")
		b, err := experiencebooking.NewBooking(expID, hostID, uuid.New(), sess, 2, 8, price, 0.10)
		if err != nil {
			t.Fatalf("NewBooking: %v", err)
		}
		return b
	}

	// Active booking, 14:00 → 16:00.
	active := mkBooking(base)
	if err := repo.Create(ctx, active); err != nil {
		t.Fatalf("Create active: %v", err)
	}

	// Cancelled booking starting at the same time, 14:00 → 16:00, must
	// be excluded from overlap hits so the freed slot can be re-booked.
	cancelled := mkBooking(base)
	if err := cancelled.Cancel(time.Now().UTC()); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := repo.Create(ctx, cancelled); err != nil {
		t.Fatalf("Create cancelled: %v", err)
	}

	// A booking against a different experience at the same time must
	// not surface — overlap is scoped per experience.
	other := newTestExperienceBooking(t, pool, 72*time.Hour, 2)
	other.Session.StartAt = base
	if err := repo.Create(ctx, other); err != nil {
		t.Fatalf("Create other: %v", err)
	}

	cases := []struct {
		name      string
		start     time.Time
		end       time.Time
		wantCount int
	}{
		// Identical window — full overlap.
		{"identical", base, base.Add(2 * time.Hour), 1},
		// Query window contains the booking window.
		{"contains", base.Add(-30 * time.Minute), base.Add(3 * time.Hour), 1},
		// Query window inside the booking window.
		{"inside", base.Add(30 * time.Minute), base.Add(90 * time.Minute), 1},
		// Half-open: query ends exactly when booking starts → no overlap.
		{"ends_at_start", base.Add(-2 * time.Hour), base, 0},
		// Half-open: query starts exactly when booking ends → no overlap.
		{"starts_at_end", base.Add(2 * time.Hour), base.Add(4 * time.Hour), 0},
		// Completely before → no overlap.
		{"strictly_before", base.Add(-4 * time.Hour), base.Add(-1 * time.Hour), 0},
		// Completely after → no overlap.
		{"strictly_after", base.Add(3 * time.Hour), base.Add(5 * time.Hour), 0},
		// Tail edge overlap — query straddles booking end by 1 minute.
		{"tail_overlap", base.Add(110 * time.Minute), base.Add(3 * time.Hour), 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := repo.FindOverlapping(ctx, expID, c.start, c.end)
			if err != nil {
				t.Fatalf("FindOverlapping: %v", err)
			}
			if len(got) != c.wantCount {
				ids := make([]string, 0, len(got))
				for _, b := range got {
					ids = append(ids, b.ID.String()+"("+string(b.Status)+")")
				}
				t.Errorf("count = %d, want %d (hits=%v)", len(got), c.wantCount, ids)
			}
			for _, b := range got {
				if b.Status == experiencebooking.StatusCancelled {
					t.Errorf("cancelled booking %s leaked into overlap hits", b.ID)
				}
				if b.ID == other.ID {
					t.Errorf("other-experience booking %s leaked into overlap hits", b.ID)
				}
			}
		})
	}
}
