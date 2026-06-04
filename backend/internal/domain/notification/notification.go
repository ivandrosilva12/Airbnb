// Package notification is the bounded context for in-app notifications. A
// Notification is delivered to a single recipient and references the resource
// that triggered it.
package notification

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Type classifies a notification so clients can route or icon it.
type Type string

const (
	TypeBookingRequested Type = "booking_requested"
	TypeBookingConfirmed Type = "booking_confirmed"
	TypeBookingCancelled Type = "booking_cancelled"
	TypeBookingModified  Type = "booking_modified"
	TypeMessageReceived  Type = "message_received"
	TypeIdentityVerified Type = "identity_verified"
	TypeReviewRequested  Type = "review_requested"
	TypeSavedSearchAlert Type = "saved_search_alert"
	TypeDisputeOpened    Type = "dispute_opened"
	TypeDisputeResolved  Type = "dispute_resolved"
	// Experience-booking lifecycle (S86 — wires ExperienceBooking events
	// into the notification fan-out so guests/hosts see the same prompts
	// as for property bookings).
	TypeExperienceBookingRequested Type = "experience_booking_requested"
	TypeExperienceBookingConfirmed Type = "experience_booking_confirmed"
	TypeExperienceBookingCancelled Type = "experience_booking_cancelled"
	// TypeSplitPaymentCompleted fires when every share of a split-payment
	// plan has been authorised (S93 — WF-GAP-011). The organizer (who set
	// the split up) and every other payer receive one, so the whole group
	// knows the trip is booked.
	TypeSplitPaymentCompleted Type = "split_payment_completed"
	// Offer lifecycle (S99 — WF-GAP-008). OfferReceived is sent to the
	// guest when a host opens a pre-approval / special offer; OfferDeclined
	// tells the host their offer was rejected; OfferWithdrawn tells the
	// guest the host took the offer back before they acted on it.
	TypeOfferReceived  Type = "offer_received"
	TypeOfferDeclined  Type = "offer_declined"
	TypeOfferWithdrawn Type = "offer_withdrawn"
	// TypeCohostInvited fires when a host grants a user one or more cohost
	// permissions on a listing (S99 — WF-GAP-016). The invitee is notified
	// so the grant doesn't sit silently in their cohost mailbox.
	TypeCohostInvited Type = "cohost_invited"
	// TypeArrivalInfoAvailable fires once a confirmed booking crosses
	// into the 48-hour reveal window (S102 — WF-GAP-007). The guest is
	// nudged to check the listing for check-in instructions, wifi, etc.
	TypeArrivalInfoAvailable Type = "arrival_info_available"
)

// Notification is the aggregate root for a single in-app notification.
type Notification struct {
	ID        uuid.UUID
	UserID    uuid.UUID // recipient
	Type      Type
	Title     string
	Body      string
	RelatedID uuid.UUID // related resource (booking, conversation, …); may be Nil
	ReadAt    *time.Time
	CreatedAt time.Time
}

// New builds a Notification for a recipient, enforcing basic invariants.
func New(userID uuid.UUID, t Type, title, body string, relatedID uuid.UUID) (*Notification, error) {
	title = strings.TrimSpace(title)
	if userID == uuid.Nil {
		return nil, shared.NewValidationError("notification requires a recipient")
	}
	if title == "" {
		return nil, shared.NewValidationError("notification requires a title")
	}
	return &Notification{
		ID:        uuid.New(),
		UserID:    userID,
		Type:      t,
		Title:     title,
		Body:      strings.TrimSpace(body),
		RelatedID: relatedID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// MarkRead stamps the notification as read (idempotent).
func (n *Notification) MarkRead() {
	if n.ReadAt == nil {
		now := time.Now().UTC()
		n.ReadAt = &now
	}
}

// MarkUnread clears the read stamp so the notification resurfaces as unread
// (idempotent).
func (n *Notification) MarkUnread() {
	n.ReadAt = nil
}

// IsRead reports whether the notification has been read.
func (n *Notification) IsRead() bool { return n.ReadAt != nil }
