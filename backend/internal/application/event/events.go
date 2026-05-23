package event

import "github.com/google/uuid"

// BookingRequested is published when a guest creates a booking. The host should
// be notified, and the payment context authorizes the total.
type BookingRequested struct {
	BookingID     uuid.UUID
	PropertyID    uuid.UUID
	PropertyTitle string
	HostID        uuid.UUID
	GuestID       uuid.UUID
	TotalCents    int64
	Currency      string
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

// MessageSent is published when a message is posted; the recipient is notified.
type MessageSent struct {
	ConversationID uuid.UUID
	SenderID       uuid.UUID
	RecipientID    uuid.UUID
}

func (MessageSent) EventName() string { return "message.sent" }
