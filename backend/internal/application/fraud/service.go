// Package fraudapp wires the domain fraud assessor to the application
// layer. The Service hydrates AssessmentInput from the booking + user
// + identity reads and persists the resulting Assessment. It is
// deliberately best-effort: the booking-create HTTP handler calls
// Assess after a successful booking and logs (but does not surface)
// any error. Fraud detection is forensic, not blocking.
package fraudapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/fraud"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// IdentityVerifier mirrors the booking-app port. Defined here so the
// fraud BC stays decoupled from the booking BC.
type IdentityVerifier interface {
	IsVerified(ctx context.Context, userID uuid.UUID) (bool, error)
}

// Service assembles an Assessment from booking + user + identity
// reads and persists it.
type Service struct {
	repo       fraud.Repository
	bookings   booking.Repository
	users      user.Repository
	identity   IdentityVerifier
	// highValueThresholds is the per-currency table the assessor
	// consults for the high_value rule. Defensive-copied at wire
	// time so post-wire mutation can't reshape running behaviour.
	highValueThresholds map[string]int64
}

// NewService builds a fraud application service.
//
// highValueThresholds is the per-currency map (e.g. {"EUR":500000})
// used by the high-value rule. Passing nil/empty disables that rule
// without disabling the rest of the assessor.
func NewService(repo fraud.Repository, bookings booking.Repository, users user.Repository, identity IdentityVerifier, highValueThresholds map[string]int64) *Service {
	s := &Service{repo: repo, bookings: bookings, users: users, identity: identity}
	if len(highValueThresholds) > 0 {
		cp := make(map[string]int64, len(highValueThresholds))
		for k, v := range highValueThresholds {
			cp[k] = v
		}
		s.highValueThresholds = cp
	}
	return s
}

// Assess scores a booking that has just been created. Reads the
// booking + guest, runs the domain rules, persists the resulting
// Assessment. Returns the Assessment for callers that want to log
// "level=high triggered by booking X" without re-fetching.
//
// All required reads use the booking ID — the caller doesn't need
// to thread guest + property through the handler. The cost is one
// extra booking lookup right after Create returned; acceptable
// because Assess runs off the booking-create hot path is best-
// effort (failure is logged and swallowed).
func (s *Service) Assess(ctx context.Context, bookingID uuid.UUID) (*fraud.Assessment, error) {
	b, err := s.bookings.FindByID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	u, err := s.users.FindByID(ctx, b.GuestID)
	if err != nil {
		return nil, err
	}
	verified := false
	if s.identity != nil {
		v, err := s.identity.IsVerified(ctx, b.GuestID)
		if err == nil {
			verified = v
		}
	}
	// Recent-booking count for the velocity rule. We fetch one page
	// of the guest's bookings (newest first) and count those within
	// the velocity window. Page size of 50 is enough headroom — any
	// guest with >50 bookings in 24h is already maximally flagged
	// regardless of the exact count.
	recent := 0
	if list, lerr := s.bookings.ListByGuest(ctx, b.GuestID, shared.NewPage(50, 0)); lerr == nil {
		cutoff := time.Now().UTC().Add(-fraud.GuestVelocityWindow)
		for _, bb := range list.Items {
			if bb.ID == b.ID {
				continue // don't count the booking we are scoring
			}
			if bb.CreatedAt.After(cutoff) {
				recent++
			}
		}
	}

	signals := fraud.Assess(fraud.AssessmentInput{
		GuestCreatedAt:          u.CreatedAt,
		GuestKYCVerified:        verified,
		GuestIsActive:           u.IsActive,
		GuestRecentBookingCount: recent,
		BookingTotalCents:       b.Pricing.Total.AmountCents(),
		BookingCurrency:         b.Pricing.Total.Currency(),
		HighValueThresholds:     s.highValueThresholds,
		BookingCheckIn:          b.Dates.CheckIn,
		BookingCreatedAt:        b.CreatedAt,
	})
	a, err := fraud.New(b.ID, b.GuestID, signals, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// List returns assessments matching the filter, page-bounded. Used by
// the admin queue ("show me high-risk bookings to review").
func (s *Service) List(ctx context.Context, f fraud.ListFilter, page shared.Page) (shared.PageResult[*fraud.Assessment], error) {
	return s.repo.List(ctx, f, page)
}

// GetByBookingID returns the (at most one) assessment for a booking,
// for the admin booking-detail surface.
func (s *Service) GetByBookingID(ctx context.Context, bookingID uuid.UUID) (*fraud.Assessment, error) {
	return s.repo.FindByBookingID(ctx, bookingID)
}
