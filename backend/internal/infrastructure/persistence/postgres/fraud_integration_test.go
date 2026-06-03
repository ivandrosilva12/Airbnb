// Postgres integration tests for the fraud.Repository (S72). Mirrors
// the env-skip pattern from S41/S55 — opt in via POSTGRES_TEST_DSN,
// run from CI's postgres-integration job.
//
// The in-memory adapter gives the application-level contract; this
// file covers what the in-memory path cannot:
//
//   - JSONB round-trips signals faithfully — name + impact + note
//     come back intact through the encoding/json pgx path.
//   - Save is upsert-on-booking_id: re-scoring the same booking
//     replaces the prior row rather than 23505-erroring on the UNIQUE
//     constraint, and the returned id matches the latest assessment.
//   - The ListFilter.MinLevel boundary is enforced in SQL: filtering
//     by medium excludes low, filtering by high excludes medium.
//   - ORDER BY (created_at DESC, id ASC) matches the memory adapter so
//     swapping backends doesn't reshuffle the admin's paged view.
//
// Run locally with:
//
//	POSTGRES_TEST_DSN='postgres://airhost:airhost@localhost:5432/airhost?sslmode=disable' \
//	  go test ./internal/infrastructure/persistence/postgres/... -run Integration_Fraud
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/fraud"
	"github.com/airhost/backend/internal/domain/shared"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
)

// TestIntegration_Fraud_SaveFindByBooking_RoundTripsSignals commits one
// assessment with a typed signal slice, reads it back via FindByBookingID,
// and asserts the score/level/signals shape survived the JSONB hop.
func TestIntegration_Fraud_SaveFindByBooking_RoundTripsSignals(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "fraud_assessments")
	repo := pg.NewFraudRepository(pool)
	ctx := context.Background()

	bookingID, guestID := uuid.New(), uuid.New()
	a, err := fraud.New(bookingID, guestID, []fraud.Signal{
		{Name: fraud.SignalNameNewAccount, Impact: 20, Note: "fresh account"},
		{Name: fraud.SignalNameShortLead, Impact: 15, Note: "short lead"},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("fraud.New: %v", err)
	}
	if err := repo.Save(ctx, a); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		t.Fatalf("FindByBookingID: %v", err)
	}
	if got.ID != a.ID || got.BookingID != bookingID || got.GuestID != guestID {
		t.Errorf("typed fields lost: %+v", got)
	}
	if got.Score != a.Score {
		t.Errorf("score = %d, want %d", got.Score, a.Score)
	}
	if got.Level != a.Level {
		t.Errorf("level = %s, want %s", got.Level, a.Level)
	}
	if len(got.Signals) != 2 {
		t.Fatalf("signals len = %d, want 2 (%+v)", len(got.Signals), got.Signals)
	}
	// Signals are sorted descending by impact in the domain New constructor.
	// Confirm both name + impact + note round-tripped.
	if got.Signals[0].Name != fraud.SignalNameNewAccount || got.Signals[0].Impact != 20 || got.Signals[0].Note != "fresh account" {
		t.Errorf("signal[0] lost: %+v", got.Signals[0])
	}
	if got.Signals[1].Name != fraud.SignalNameShortLead || got.Signals[1].Impact != 15 {
		t.Errorf("signal[1] lost: %+v", got.Signals[1])
	}
}

// TestIntegration_Fraud_FindByBooking_NotFound returns shared.ErrNotFound
// when nothing matches the booking id — the handler relies on this to
// answer 404 cleanly instead of a generic 500.
func TestIntegration_Fraud_FindByBooking_NotFound(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "fraud_assessments")
	repo := pg.NewFraudRepository(pool)
	ctx := context.Background()

	_, err := repo.FindByBookingID(ctx, uuid.New())
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("err = %v, want shared.ErrNotFound", err)
	}
}

// TestIntegration_Fraud_Save_UpsertsByBookingID — re-scoring the same
// booking must replace the prior row, not raise 23505 on the UNIQUE
// constraint. The latest assessment's id wins so a follow-up
// FindByBookingID returns the freshest row's id (matching the in-
// memory adapter's behaviour and what BookingHandler.Create logs).
func TestIntegration_Fraud_Save_UpsertsByBookingID(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "fraud_assessments")
	repo := pg.NewFraudRepository(pool)
	ctx := context.Background()

	bookingID, guestID := uuid.New(), uuid.New()
	first, err := fraud.New(bookingID, guestID, []fraud.Signal{
		{Name: fraud.SignalNameNewAccount, Impact: 20, Note: ""},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("fraud.New first: %v", err)
	}
	if err := repo.Save(ctx, first); err != nil {
		t.Fatalf("Save first: %v", err)
	}

	// Re-Assess with stronger signals → high-risk.
	second, err := fraud.New(bookingID, guestID, []fraud.Signal{
		{Name: fraud.SignalNameSuspended, Impact: 60, Note: ""},
		{Name: fraud.SignalNameGuestVelocity, Impact: 30, Note: ""},
	}, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatalf("fraud.New second: %v", err)
	}
	if err := repo.Save(ctx, second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	got, err := repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		t.Fatalf("FindByBookingID: %v", err)
	}
	if got.ID != second.ID {
		t.Errorf("id = %s, want second (%s) — upsert did not replace id", got.ID, second.ID)
	}
	if got.Score != second.Score {
		t.Errorf("score = %d, want %d", got.Score, second.Score)
	}
	if got.Level != fraud.LevelHigh {
		t.Errorf("level = %s, want high", got.Level)
	}

	// Total row count is 1 — upsert, not append.
	res, err := repo.List(ctx, fraud.ListFilter{}, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Total != 1 || len(res.Items) != 1 {
		t.Errorf("row count = %d/%d, want 1/1 (upsert behaviour)", res.Total, len(res.Items))
	}
}

// TestIntegration_Fraud_List_LevelFilter_EnforcedInSQL seeds one row at
// each level and confirms that filtering by medium returns medium+high
// only, and filtering by high returns just high. Catches a regression
// where the WHERE clause is built wrong (or empty filter accidentally
// hides rows).
func TestIntegration_Fraud_List_LevelFilter_EnforcedInSQL(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "fraud_assessments")
	repo := pg.NewFraudRepository(pool)
	ctx := context.Background()

	// low: single 20-impact signal (score 20)
	low, _ := fraud.New(uuid.New(), uuid.New(), []fraud.Signal{
		{Name: "low_test", Impact: 20, Note: ""},
	}, time.Now().UTC())
	// medium: 35 score (in [30,70))
	med, _ := fraud.New(uuid.New(), uuid.New(), []fraud.Signal{
		{Name: "med_test", Impact: 35, Note: ""},
	}, time.Now().UTC().Add(time.Millisecond))
	// high: 80 score (>= 70)
	high, _ := fraud.New(uuid.New(), uuid.New(), []fraud.Signal{
		{Name: "high_test", Impact: 80, Note: ""},
	}, time.Now().UTC().Add(2*time.Millisecond))
	for _, a := range []*fraud.Assessment{low, med, high} {
		if err := repo.Save(ctx, a); err != nil {
			t.Fatalf("Save %s: %v", a.Level, err)
		}
	}

	cases := []struct {
		filter fraud.Level
		want   int
	}{
		{"", 3},               // empty filter — every row
		{fraud.LevelLow, 3},   // low floor — every row
		{fraud.LevelMedium, 2},
		{fraud.LevelHigh, 1},
	}
	for _, c := range cases {
		t.Run(string(c.filter), func(t *testing.T) {
			res, err := repo.List(ctx, fraud.ListFilter{MinLevel: c.filter}, shared.NewPage(10, 0))
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if int(res.Total) != c.want {
				t.Errorf("total = %d, want %d", res.Total, c.want)
			}
			if len(res.Items) != c.want {
				t.Errorf("items = %d, want %d", len(res.Items), c.want)
			}
		})
	}

	// Pin the sort: every returned item in the high filter has level=high,
	// and the unfiltered list orders by created_at DESC (high → med → low).
	res, _ := repo.List(ctx, fraud.ListFilter{}, shared.NewPage(10, 0))
	if len(res.Items) >= 3 {
		if res.Items[0].Level != fraud.LevelHigh ||
			res.Items[1].Level != fraud.LevelMedium ||
			res.Items[2].Level != fraud.LevelLow {
			var got []string
			for _, it := range res.Items {
				got = append(got, string(it.Level))
			}
			t.Errorf("order = %v, want [high medium low] (created_at DESC)", got)
		}
	}
}
