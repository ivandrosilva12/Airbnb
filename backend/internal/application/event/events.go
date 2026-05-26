package event

import "github.com/google/uuid"

// init registers a JSON decoder for every event type so the outbox can
// reconstruct persisted records for re-delivery.
func init() {
	Register(BookingRequested{}.EventName(), jsonDecoder[BookingRequested]())
	Register(BookingConfirmed{}.EventName(), jsonDecoder[BookingConfirmed]())
	Register(BookingCancelled{}.EventName(), jsonDecoder[BookingCancelled]())
	Register(BookingCompleted{}.EventName(), jsonDecoder[BookingCompleted]())
	Register(BookingModified{}.EventName(), jsonDecoder[BookingModified]())
	Register(MessageSent{}.EventName(), jsonDecoder[MessageSent]())
	Register(IdentityVerified{}.EventName(), jsonDecoder[IdentityVerified]())
}

// BookingRequested is published when a guest creates a booking. The host should
// be notified, and the payment context authorizes the total. Instant is true
// when the listing has instant-book enabled, in which case a BookingConfirmed
// event is published in the same transaction and the booking starts confirmed.
type BookingRequested struct {
	BookingID     uuid.UUID
	PropertyID    uuid.UUID
	PropertyTitle string
	HostID        uuid.UUID
	GuestID       uuid.UUID
	TotalCents    int64
	Currency      string
	Instant       bool
}

func (BookingRequested) EventName() string { return "booking.requested" }

// BookingConfirmed is published when a host confirms a booking. The guest
// should be notified.
type BookingConfirmed struct {
	BookingID     uuid.UUID
	PropertyID    uuid.UUID
	PropertyTitle string
	GuestID       uuid.UUID
}

func (BookingConfirmed) EventName() string { return "booking.confirmed" }

// BookingCancelled is published when either party cancels. The other party is
// notified, and the payment context refunds RefundFraction of any captured
// amount (a hold that was only authorized is always released in full).
type BookingCancelled struct {
	BookingID      uuid.UUID
	PropertyID     uuid.UUID
	PropertyTitle  string
	HostID         uuid.UUID
	GuestID        uuid.UUID
	CancelledBy    uuid.UUID
	RefundFraction float64 // 0..1, applied to captured payments
}

func (BookingCancelled) EventName() string { return "booking.cancelled" }

// BookingCompleted is published when a host marks a stay completed (after
// check-out). Both parties are prompted to leave a post-stay review.
type BookingCompleted struct {
	BookingID     uuid.UUID
	PropertyID    uuid.UUID
	PropertyTitle string
	HostID        uuid.UUID
	GuestID       uuid.UUID
}

func (BookingCompleted) EventName() string { return "booking.completed" }

// BookingModified is published when a guest changes the dates and/or guest count
// of a still-pending booking. The host is notified, and the payment context
// adjusts the outstanding authorization hold to the new total.
type BookingModified struct {
	BookingID     uuid.UUID
	PropertyID    uuid.UUID
	PropertyTitle string
	HostID        uuid.UUID
	GuestID       uuid.UUID
	TotalCents    int64
	Currency      string
}

func (BookingModified) EventName() string { return "booking.modified" }

// MessageSent is published when a message is posted; the recipient is notified.
type MessageSent struct {
	ConversationID uuid.UUID
	SenderID       uuid.UUID
	RecipientID    uuid.UUID
}

func (MessageSent) EventName() string { return "message.sent" }

// IdentityVerified is published when an administrator approves a user's KYC
// identity-verification request. The user is notified and emailed.
type IdentityVerified struct {
	VerificationID uuid.UUID
	UserID         uuid.UUID
}

func (IdentityVerified) EventName() string { return "identity.verified" }
