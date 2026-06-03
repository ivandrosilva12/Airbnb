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
	"github.com/google/uuid"
)

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
}

// NewService wires the application service. serviceFeeRate is the
// platform's cut applied on top of subtotal (e.g. 0.10 for 10 %).
func NewService(repo experiencebooking.Repository, experiences ExperienceFinder, serviceFeeRate float64) *Service {
	return &Service{repo: repo, experiences: experiences, serviceFeeRate: serviceFeeRate}
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
	if err := b.Cancel(time.Now().UTC()); err != nil {
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
	if err := b.Complete(time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}
