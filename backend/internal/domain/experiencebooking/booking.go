// Package experiencebooking owns the ExperienceBooking aggregate — a
// guest's reservation of a host-led activity (a session). Modelled as a
// separate BC from booking (the property reservation) because the
// invariants are different: experiences are single-session (one
// scheduled start), priced per-guest, with no overnight semantics, no
// cleaning fee, and a hard cap from the parent experience's MaxGuests.
//
// Slice S80 ships only the in-memory aggregate + service. Postgres
// persistence, refund-on-cancel, and the seller-side completion workflow
// land in later slices once the per-session availability model is
// settled.
package experiencebooking

import (
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Status is the reservation lifecycle. Same shape as booking.Status so
// the host UI can render "pending"/"confirmed"/"cancelled"/"completed"
// without per-BC translation.
type Status string

const (
	StatusPending   Status = "pending"
	StatusConfirmed Status = "confirmed"
	StatusCancelled Status = "cancelled"
	StatusCompleted Status = "completed"
)

// Valid reports whether s is one of the recognised states.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusConfirmed, StatusCancelled, StatusCompleted:
		return true
	}
	return false
}

// Session is the scheduled run of the activity. StartAt is the moment
// the guests should arrive; DurationMinutes is copied from the parent
// experience so the booking is self-contained even if the host later
// edits the listing's duration. EndAt() is a derived accessor.
type Session struct {
	StartAt         time.Time
	DurationMinutes int
}

// NewSession constructs a validated Session. The start time must be in
// the future and the duration must be within the experience-level
// bounds (caller passes the experience's own tunables).
func NewSession(startAt time.Time, durationMinutes int, minDuration, maxDuration int) (Session, error) {
	startAt = startAt.UTC()
	if !startAt.After(time.Now().UTC()) {
		return Session{}, shared.NewValidationError("experiencebooking: session start must be in the future")
	}
	if durationMinutes < minDuration || durationMinutes > maxDuration {
		return Session{}, shared.NewValidationError("experiencebooking: session duration out of allowed range")
	}
	return Session{StartAt: startAt, DurationMinutes: durationMinutes}, nil
}

// EndAt is the moment the session is scheduled to finish.
func (s Session) EndAt() time.Time {
	return s.StartAt.Add(time.Duration(s.DurationMinutes) * time.Minute)
}

// Booking is the aggregate root. Pricing is derived in the domain —
// callers never supply the total.
type Booking struct {
	ID           uuid.UUID
	ExperienceID uuid.UUID
	HostID       uuid.UUID
	GuestID      uuid.UUID
	Session      Session
	Guests       int
	Pricing      Pricing
	Status       Status
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewBooking creates a pending Booking for an experience session. The
// caller supplies the parent experience's per-guest price, host id, and
// the session window; the domain checks bounds and computes the price.
//
// guestsCap is the parent experience's MaxGuests — enforced here so a
// caller cannot oversell the activity through the back door.
func NewBooking(
	experienceID, hostID, guestID uuid.UUID,
	session Session,
	guests int, guestsCap int,
	pricePerGuest shared.Money,
	serviceFeeRate float64,
) (*Booking, error) {
	if experienceID == uuid.Nil {
		return nil, shared.NewValidationError("experiencebooking: experienceID is required")
	}
	if hostID == uuid.Nil {
		return nil, shared.NewValidationError("experiencebooking: hostID is required")
	}
	if guestID == uuid.Nil {
		return nil, shared.NewValidationError("experiencebooking: guestID is required")
	}
	if hostID == guestID {
		return nil, shared.NewValidationError("experiencebooking: host cannot book own experience")
	}
	if guests < 1 {
		return nil, shared.NewValidationError("experiencebooking: at least one guest is required")
	}
	if guestsCap > 0 && guests > guestsCap {
		return nil, shared.NewValidationError("experiencebooking: guests exceeds experience capacity")
	}
	pricing, err := ComputePricing(pricePerGuest, guests, serviceFeeRate)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Booking{
		ID:           uuid.New(),
		ExperienceID: experienceID,
		HostID:       hostID,
		GuestID:      guestID,
		Session:      session,
		Guests:       guests,
		Pricing:      pricing,
		Status:       StatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// Confirm moves a pending booking to confirmed. Idempotent on the
// confirmed state; refuses to walk back from terminal states.
func (b *Booking) Confirm() error {
	switch b.Status {
	case StatusConfirmed:
		return nil
	case StatusPending:
		b.Status = StatusConfirmed
		b.touch()
		return nil
	default:
		return shared.NewValidationError("experiencebooking: cannot confirm a " + string(b.Status) + " booking")
	}
}

// Cancel moves the booking to cancelled. Allowed only before the
// session starts; once the session has begun the booking belongs to
// the completion workflow.
//
// Refund computation is intentionally NOT modelled in this slice — the
// dispute / partial-refund flow will own it once the cancellation
// policy ladder is decided for experiences.
func (b *Booking) Cancel(now time.Time) error {
	switch b.Status {
	case StatusCancelled:
		return nil
	case StatusCompleted:
		return shared.NewValidationError("experiencebooking: completed booking cannot be cancelled")
	}
	if !now.Before(b.Session.StartAt) {
		return shared.NewValidationError("experiencebooking: cannot cancel after session start")
	}
	b.Status = StatusCancelled
	b.touch()
	return nil
}

// Complete marks a confirmed booking as completed. Only callable once
// the session window has elapsed (start + duration). The host-side
// review-request workflow listens for this transition.
func (b *Booking) Complete(now time.Time) error {
	if b.Status == StatusCompleted {
		return nil
	}
	if b.Status != StatusConfirmed {
		return shared.NewValidationError("experiencebooking: only confirmed bookings can be completed")
	}
	if now.Before(b.Session.EndAt()) {
		return shared.NewValidationError("experiencebooking: session has not finished yet")
	}
	b.Status = StatusCompleted
	b.touch()
	return nil
}

func (b *Booking) touch() {
	b.UpdatedAt = time.Now().UTC()
}
