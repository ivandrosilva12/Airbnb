package fraud_test

import (
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/fraud"
	"github.com/google/uuid"
)

// fixed time helper — keeps every test independent of the wall clock.
var fixedNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func TestAssess_NoSignals(t *testing.T) {
	// A long-standing, verified, active, low-value booking with a
	// generous lead window should produce zero signals.
	signals := fraud.Assess(fraud.AssessmentInput{
		GuestCreatedAt:    fixedNow.AddDate(-1, 0, 0),
		GuestKYCVerified:  true,
		GuestIsActive:     true,
		BookingTotalCents: 10000,
		BookingCurrency:   "EUR",
		BookingCheckIn:    fixedNow.AddDate(0, 0, 30),
		BookingCreatedAt:  fixedNow,
	})
	if len(signals) != 0 {
		t.Fatalf("expected no signals, got %v", signals)
	}
}

func TestAssess_NewAccount(t *testing.T) {
	signals := fraud.Assess(fraud.AssessmentInput{
		GuestCreatedAt:   fixedNow.Add(-2 * time.Hour),
		GuestKYCVerified: true,
		GuestIsActive:    true,
		BookingCheckIn:   fixedNow.AddDate(0, 0, 30),
		BookingCreatedAt: fixedNow,
	})
	if !hasSignal(signals, fraud.SignalNameNewAccount) {
		t.Fatalf("expected new_account signal, got %v", signals)
	}
}

func TestAssess_ShortLead(t *testing.T) {
	signals := fraud.Assess(fraud.AssessmentInput{
		GuestCreatedAt:   fixedNow.AddDate(-1, 0, 0),
		GuestKYCVerified: true,
		GuestIsActive:    true,
		// Check-in in 1 day = short-lead window (3d).
		BookingCheckIn:   fixedNow.Add(24 * time.Hour),
		BookingCreatedAt: fixedNow,
	})
	if !hasSignal(signals, fraud.SignalNameShortLead) {
		t.Fatalf("expected short_lead signal, got %v", signals)
	}
}

func TestAssess_HighValue(t *testing.T) {
	signals := fraud.Assess(fraud.AssessmentInput{
		GuestCreatedAt:      fixedNow.AddDate(-1, 0, 0),
		GuestKYCVerified:    true,
		GuestIsActive:       true,
		BookingTotalCents:   600000, // 6000 EUR
		BookingCurrency:     "EUR",
		HighValueThresholds: map[string]int64{"EUR": 500000},
		BookingCheckIn:      fixedNow.AddDate(0, 0, 30),
		BookingCreatedAt:    fixedNow,
	})
	if !hasSignal(signals, fraud.SignalNameHighValue) {
		t.Fatalf("expected high_value signal, got %v", signals)
	}
}

func TestAssess_HighValue_NoThresholdForCurrency_SkipsRule(t *testing.T) {
	signals := fraud.Assess(fraud.AssessmentInput{
		GuestCreatedAt:      fixedNow.AddDate(-1, 0, 0),
		GuestKYCVerified:    true,
		GuestIsActive:       true,
		BookingTotalCents:   600000,
		BookingCurrency:     "AOA",
		HighValueThresholds: map[string]int64{"EUR": 500000}, // no AOA entry
		BookingCheckIn:      fixedNow.AddDate(0, 0, 30),
		BookingCreatedAt:    fixedNow,
	})
	if hasSignal(signals, fraud.SignalNameHighValue) {
		t.Fatalf("high_value should not fire for an unconfigured currency, got %v", signals)
	}
}

func TestAssess_MissingKYC(t *testing.T) {
	signals := fraud.Assess(fraud.AssessmentInput{
		GuestCreatedAt:   fixedNow.AddDate(-1, 0, 0),
		GuestKYCVerified: false,
		GuestIsActive:    true,
		BookingCheckIn:   fixedNow.AddDate(0, 0, 30),
		BookingCreatedAt: fixedNow,
	})
	if !hasSignal(signals, fraud.SignalNameMissingKYC) {
		t.Fatalf("expected missing_kyc signal, got %v", signals)
	}
}

func TestAssess_Velocity(t *testing.T) {
	signals := fraud.Assess(fraud.AssessmentInput{
		GuestCreatedAt:          fixedNow.AddDate(-1, 0, 0),
		GuestKYCVerified:        true,
		GuestIsActive:           true,
		GuestRecentBookingCount: fraud.GuestVelocityLimit + 1,
		BookingCheckIn:          fixedNow.AddDate(0, 0, 30),
		BookingCreatedAt:        fixedNow,
	})
	if !hasSignal(signals, fraud.SignalNameGuestVelocity) {
		t.Fatalf("expected guest_velocity signal, got %v", signals)
	}
}

func TestAssess_VelocityAtLimit_DoesNotFire(t *testing.T) {
	signals := fraud.Assess(fraud.AssessmentInput{
		GuestCreatedAt:          fixedNow.AddDate(-1, 0, 0),
		GuestKYCVerified:        true,
		GuestIsActive:           true,
		GuestRecentBookingCount: fraud.GuestVelocityLimit,
		BookingCheckIn:          fixedNow.AddDate(0, 0, 30),
		BookingCreatedAt:        fixedNow,
	})
	if hasSignal(signals, fraud.SignalNameGuestVelocity) {
		t.Fatalf("velocity at the limit should not fire, got %v", signals)
	}
}

func TestAssess_Suspended(t *testing.T) {
	signals := fraud.Assess(fraud.AssessmentInput{
		GuestCreatedAt:   fixedNow.AddDate(-1, 0, 0),
		GuestKYCVerified: true,
		GuestIsActive:    false,
		BookingCheckIn:   fixedNow.AddDate(0, 0, 30),
		BookingCreatedAt: fixedNow,
	})
	if !hasSignal(signals, fraud.SignalNameSuspended) {
		t.Fatalf("expected suspended_guest signal, got %v", signals)
	}
}

func TestNew_ClampsScoreAtHundred(t *testing.T) {
	a, err := fraud.New(uuid.New(), uuid.New(), []fraud.Signal{
		{Name: "a", Impact: 80, Note: ""},
		{Name: "b", Impact: 40, Note: ""},
		{Name: "c", Impact: 30, Note: ""},
	}, fixedNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Score != 100 {
		t.Fatalf("score = %d, want clamped 100", a.Score)
	}
	if a.Level != fraud.LevelHigh {
		t.Fatalf("level = %s, want high", a.Level)
	}
}

func TestNew_SortsSignalsByImpactDescending(t *testing.T) {
	a, err := fraud.New(uuid.New(), uuid.New(), []fraud.Signal{
		{Name: "low_impact", Impact: 10, Note: ""},
		{Name: "high_impact", Impact: 60, Note: ""},
		{Name: "mid_impact", Impact: 25, Note: ""},
	}, fixedNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"high_impact", "mid_impact", "low_impact"}
	for i, n := range want {
		if a.Signals[i].Name != n {
			t.Fatalf("signal[%d] = %s, want %s (full: %v)", i, a.Signals[i].Name, n, a.Signals)
		}
	}
}

func TestLevelFromScore(t *testing.T) {
	cases := []struct {
		score int
		want  fraud.Level
	}{
		{0, fraud.LevelLow},
		{29, fraud.LevelLow},
		{30, fraud.LevelMedium},
		{69, fraud.LevelMedium},
		{70, fraud.LevelHigh},
		{100, fraud.LevelHigh},
	}
	for _, c := range cases {
		if got := fraud.LevelFromScore(c.score); got != c.want {
			t.Errorf("LevelFromScore(%d) = %s, want %s", c.score, got, c.want)
		}
	}
}

func hasSignal(signals []fraud.Signal, name string) bool {
	for _, s := range signals {
		if s.Name == name {
			return true
		}
	}
	return false
}
