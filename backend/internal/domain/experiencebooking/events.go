package experiencebooking

import (
	"time"

	"github.com/google/uuid"
)

// ExperienceBookingCreated is the domain event published when a guest reserves
// a session of an experience. The payment / notification subscribers that will
// plug into this lifecycle (future slice) read it to authorise the per-guest
// total and notify the host of the new pending booking.
//
// All fields are captured at the moment of creation so the event is a faithful
// snapshot — subscribers should never re-read the booking aggregate to
// reconstruct what was true at the time the event was raised. The Money value
// is flattened to TotalCents+Currency rather than carrying shared.Money
// directly because the Money type's unexported fields don't round-trip through
// JSON — that matters once this BC moves onto the durable outbox path.
type ExperienceBookingCreated struct {
	BookingID       uuid.UUID
	ExperienceID    uuid.UUID
	ExperienceTitle string // denormalised snapshot so subscribers don't re-read the experience aggregate
	HostID          uuid.UUID
	GuestID         uuid.UUID
	TotalCents      int64
	Currency        string
	OccurredAt      time.Time
}

// EventName returns a stable identifier for the event. The "experiencebooking."
// prefix mirrors the property-booking events ("booking.requested", etc.) so
// subscribers can filter by bounded context.
func (ExperienceBookingCreated) EventName() string { return "experiencebooking.created" }

// ExperienceBookingConfirmed is published when the host transitions a pending
// booking to confirmed. Future payment-capture and guest-notification
// subscribers consume it; this slice only emits — no subscribers wire in yet.
type ExperienceBookingConfirmed struct {
	BookingID       uuid.UUID
	ExperienceID    uuid.UUID
	ExperienceTitle string
	HostID          uuid.UUID
	GuestID         uuid.UUID
	OccurredAt      time.Time
}

// EventName returns the stable identifier for the confirmed event.
func (ExperienceBookingConfirmed) EventName() string { return "experiencebooking.confirmed" }

// ExperienceBookingCancelled is published when either party cancels a booking
// (the domain refuses cancellations after the session has started, so the
// event is always raised before the activity runs). CancelledBy carries the
// actor id so a future refund subscriber can decide which side bears the cost
// when the cancellation-policy ladder for experiences is settled.
type ExperienceBookingCancelled struct {
	BookingID       uuid.UUID
	ExperienceID    uuid.UUID
	ExperienceTitle string
	HostID          uuid.UUID
	GuestID         uuid.UUID
	// CancelledBy is the actor that cancelled (guest or host). Subscribers
	// notify the OTHER party so the canceller isn't told what they just did.
	CancelledBy uuid.UUID
	OccurredAt  time.Time
}

// EventName returns the stable identifier for the cancelled event.
func (ExperienceBookingCancelled) EventName() string { return "experiencebooking.cancelled" }
