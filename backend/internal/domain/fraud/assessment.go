// Package fraud is a small bounded context that scores newly-created
// bookings against a set of fraud heuristics. The score is a number in
// [0,100]; the Level enum (low/medium/high) is a coarse bucket admins
// filter by. The context owns no business decision — it never refuses
// a booking, it only records a risk signal. Ops decides what to do with
// the score (manual review, account suspension, etc).
//
// The BC is intentionally lean for the first slice:
//
//   - No machine-learning model. Just a small set of pure rules. Easy
//     to reason about, easy to unit-test, easy to disable a misfiring
//     rule by deleting its case.
//   - No webhook out. Admins read the trail via GET; they can wire it
//     to PagerDuty later if signal volume justifies it.
//   - No persistence in postgres yet. The first slice ships memory-only
//     so we can shape the wire API before locking down a table schema.
//     A future slice promotes the repo to postgres + adds an index on
//     (level, created_at).
package fraud

import (
	"sort"
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Level is the coarse risk bucket derived from a score. Admins filter
// "show me everything high-risk" without caring about exact numbers.
// The closed enum keeps wire output predictable.
type Level string

const (
	LevelLow    Level = "low"
	LevelMedium Level = "medium"
	LevelHigh   Level = "high"
)

// Valid reports whether l is one of the recognised buckets. The zero
// value is invalid.
func (l Level) Valid() bool {
	switch l {
	case LevelLow, LevelMedium, LevelHigh:
		return true
	}
	return false
}

// LevelFromScore buckets a 0..100 score. Boundaries chosen so a single
// minor signal lands in low (8 impact, < 30 floor), two compounding
// signals land in medium (e.g. new account + high value = 40), and a
// genuinely scary combination lands in high (>= 70).
func LevelFromScore(score int) Level {
	switch {
	case score >= 70:
		return LevelHigh
	case score >= 30:
		return LevelMedium
	default:
		return LevelLow
	}
}

// Signal is one heuristic outcome attached to an Assessment. Impact is
// the points contributed to the overall score (always positive — a
// signal can flag risk, never absolve it). Note carries a human-
// readable string for admin display; the wire shape exposes it
// verbatim, so it MUST NOT carry PII like raw emails.
type Signal struct {
	Name   string // stable machine identifier, see SignalName* constants
	Impact int    // points added to the score [1,100]
	Note   string // operator-facing explanation, PII-free
}

// SignalName* enumerates the rule names the assessor can emit. Stable
// strings — dashboards and alerts will group on these, so renaming one
// breaks downstream queries. The list is closed by convention; ad-hoc
// signal names from outside this package will still persist, but
// won't appear in any predefined dashboard.
const (
	SignalNameNewAccount    = "new_account"     // guest account < 24h old
	SignalNameShortLead     = "short_lead"      // booking < 3 days from check-in
	SignalNameHighValue     = "high_value"      // total exceeds per-currency threshold
	SignalNameMissingKYC    = "missing_kyc"     // guest never verified identity
	SignalNameGuestVelocity = "guest_velocity"  // > N bookings in last 24h
	SignalNameSuspended     = "suspended_guest" // guest currently suspended
)

// Assessment is one fraud-detection record produced for a single
// booking. Immutable once persisted: the assessor runs on the
// create-booking path and the row stays as forensic evidence even if
// the booking is later cancelled / refunded.
type Assessment struct {
	ID        uuid.UUID
	BookingID uuid.UUID
	GuestID   uuid.UUID
	// Score is the clamped sum of Signal impacts. 0 means no signal
	// fired (and is still persisted so absence-of-record cannot be
	// mistaken for "not yet scored").
	Score   int
	Level   Level
	Signals []Signal
	// CreatedAt is the assessor wall-clock. The booking's CreatedAt
	// may be slightly earlier; the gap is the cost of the assessor
	// running in best-effort post-commit mode.
	CreatedAt time.Time
}

// New builds an Assessment from a pre-computed signal slice. The
// caller (the domain Assessor) is responsible for running the rules;
// New is just the constructor — it clamps the score, derives the
// Level, sorts signals by impact (highest first) so admins see what
// drove the score at a glance, and stamps the timestamp.
//
// Returns shared.ErrValidation when bookingID or guestID is the zero
// UUID, or when any signal carries a non-positive impact. nil
// signals are tolerated and produce a zero-score Assessment.
func New(bookingID, guestID uuid.UUID, signals []Signal, now time.Time) (*Assessment, error) {
	if bookingID == uuid.Nil {
		return nil, shared.NewValidationError("fraud: bookingID is required")
	}
	if guestID == uuid.Nil {
		return nil, shared.NewValidationError("fraud: guestID is required")
	}
	for _, s := range signals {
		if strings.TrimSpace(s.Name) == "" {
			return nil, shared.NewValidationError("fraud: signal name is required")
		}
		if s.Impact <= 0 {
			return nil, shared.NewValidationError("fraud: signal impact must be positive")
		}
	}
	score := 0
	for _, s := range signals {
		score += s.Impact
	}
	if score > 100 {
		score = 100
	}
	// Defensive copy + descending sort by impact so the admin UI can
	// render "top 3 reasons" without re-sorting client-side. Ties
	// broken by Name for stable ordering across runs.
	sorted := make([]Signal, len(signals))
	copy(sorted, signals)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Impact != sorted[j].Impact {
			return sorted[i].Impact > sorted[j].Impact
		}
		return sorted[i].Name < sorted[j].Name
	})
	return &Assessment{
		ID:        uuid.New(),
		BookingID: bookingID,
		GuestID:   guestID,
		Score:     score,
		Level:     LevelFromScore(score),
		Signals:   sorted,
		CreatedAt: now.UTC(),
	}, nil
}
