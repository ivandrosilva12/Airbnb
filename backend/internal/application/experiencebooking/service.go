// Package experiencebookingapp orchestrates the ExperienceBooking BC
// use cases: a guest books a session of a published experience; either
// party can cancel before the session starts; the host confirms; the
// scheduler (later slice) flips to completed after the window elapses.
//
// The service depends on the Experience BC's read port to pull the
// per-guest price, the host id, and the duration/maxGuests bounds — it
// never reaches across the boundary to mutate an experience.
package experiencebookingapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/experience"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/observability/logctx"
	"github.com/google/uuid"
)

// autoCompleteBatchLimit caps how many bookings the scheduler completes in
// a single tick. The job re-runs every few minutes, so any backlog drains
// in subsequent ticks without locking the DB for too long.
const autoCompleteBatchLimit = 500

// ExperienceFinder is the slimmest read port the service needs from
// the Experience BC. Defined here rather than imported as
// experience.Repository to keep the dependency one-way and easy to
// mock.
type ExperienceFinder interface {
	FindByID(ctx context.Context, id uuid.UUID) (*experience.Experience, error)
}

// Service is the application service for the ExperienceBooking BC.
type Service struct {
	repo           experiencebooking.Repository
	experiences    ExperienceFinder
	serviceFeeRate float64
	// now is the clock; defaults to time.Now and is overridable via
	// WithClock for tests (and for callers that need a fake clock for
	// AutoCompleteOverdue / Cancel / Complete).
	now func() time.Time
}

// Option mutates the Service at construction time.
type Option func(*Service)

// WithClock overrides the wall clock. Use in tests to set up bookings
// whose session window has already closed without violating
// NewSession's future-start invariant.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// NewService wires the application service. serviceFeeRate is the
// platform's cut applied on top of subtotal (e.g. 0.10 for 10 %).
func NewService(repo experiencebooking.Repository, experiences ExperienceFinder, serviceFeeRate float64, opts ...Option) *Service {
	s := &Service{
		repo:           repo,
		experiences:    experiences,
		serviceFeeRate: serviceFeeRate,
		now:            func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreateInput is the guest's booking request. StartAt is the session
// start; Guests is the headcount. Price/duration/host are sourced from
// the experience aggregate — the guest never supplies them.
type CreateInput struct {
	ExperienceID uuid.UUID
	GuestID      uuid.UUID
	StartAt      time.Time
	Guests       int
}

// Create looks up the parent experience, validates it is published,
// builds the session, and persists a pending booking. Returns
// shared.ErrConflict when an overlapping non-cancelled booking exists
// for the same experience (no multi-cohort sessions in this slice).
func (s *Service) Create(ctx context.Context, in CreateInput) (*experiencebooking.Booking, error) {
	exp, err := s.experiences.FindByID(ctx, in.ExperienceID)
	if err != nil {
		return nil, err
	}
	if exp.Status != experience.StatusPublished {
		return nil, shared.NewValidationError("experiencebooking: experience is not bookable")
	}
	session, err := experiencebooking.NewSession(in.StartAt, exp.DurationMinutes,
		experience.MinDurationMinutes, experience.MaxDurationMinutes)
	if err != nil {
		return nil, err
	}
	// Overlap check before NewBooking so a clash returns ErrConflict
	// rather than a price-derivation success that the upsert later
	// rejects.
	overlapping, err := s.repo.FindOverlapping(ctx, exp.ID, session.StartAt, session.EndAt())
	if err != nil {
		return nil, err
	}
	if len(overlapping) > 0 {
		return nil, shared.ErrConflict
	}
	b, err := experiencebooking.NewBooking(exp.ID, exp.HostID, in.GuestID, session, in.Guests, exp.MaxGuests,
		exp.PricePerGuest, s.serviceFeeRate)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// Get returns one booking by id; the handler asserts the actor is the
// owning guest or the host.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*experiencebooking.Booking, error) {
	return s.repo.FindByID(ctx, id)
}

// ListMine returns the calling guest's bookings, newest first.
func (s *Service) ListMine(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*experiencebooking.Booking], error) {
	return s.repo.ListByGuest(ctx, guestID, page)
}

// ListForHost returns every booking against any of the host's
// experiences, newest first. Host-side authorisation is enforced by
// the handler reading the JWT subject.
func (s *Service) ListForHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*experiencebooking.Booking], error) {
	return s.repo.ListByHost(ctx, hostID, page)
}

// Confirm flips a pending booking to confirmed. Only the host may
// call.
func (s *Service) Confirm(ctx context.Context, actorID, id uuid.UUID) (*experiencebooking.Booking, error) {
	b, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.HostID != actorID {
		return nil, shared.ErrForbidden
	}
	if err := b.Confirm(); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// Cancel cancels a booking. Either the booking's guest or the parent
// host may call. The domain refuses if the session has already
// started.
func (s *Service) Cancel(ctx context.Context, actorID, id uuid.UUID) (*experiencebooking.Booking, error) {
	b, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.GuestID != actorID && b.HostID != actorID {
		return nil, shared.ErrForbidden
	}
	if err := b.Cancel(s.now()); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// Complete flips a confirmed booking to completed once the session
// window has elapsed. Intended to be called by the scheduler, but the
// host may also call to nudge the post-stay review flow forward.
func (s *Service) Complete(ctx context.Context, actorID, id uuid.UUID) (*experiencebooking.Booking, error) {
	b, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.HostID != actorID {
		return nil, shared.ErrForbidden
	}
	if err := b.Complete(s.now()); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// AutoCompleteOverdue is the scheduler entry point: every confirmed
// booking whose session window has already ended is flipped to
// completed. Returns the count of bookings successfully completed.
//
// Errors on individual bookings (a transient DB failure on Update, or
// the defensive guard on b.Complete) are logged and skipped so one bad
// row never stalls the batch — the next tick will retry whatever did
// not flip this time around.
func (s *Service) AutoCompleteOverdue(ctx context.Context) (int, error) {
	now := s.now()
	log := logctx.LoggerFrom(ctx)
	bookings, err := s.repo.FindConfirmedPastSession(ctx, now, autoCompleteBatchLimit)
	if err != nil {
		return 0, err
	}
	completed := 0
	for _, b := range bookings {
		if err := b.Complete(now); err != nil {
			// Defensive: the where-clause already filtered to confirmed +
			// past-end so Complete should never refuse. Log and skip if it
			// does (e.g. a concurrent state change between query and now).
			log.Warn("experiencebooking: auto-complete skipped booking", "booking_id", b.ID, "status", b.Status, "error", err)
			continue
		}
		if err := s.repo.Update(ctx, b); err != nil {
			log.Error("experiencebooking: auto-complete update failed", "booking_id", b.ID, "error", err)
			continue
		}
		completed++
	}
	if completed > 0 {
		log.Info("experiencebooking: auto-completed bookings", "count", completed, "scanned", len(bookings))
	}
	return completed, nil
}
