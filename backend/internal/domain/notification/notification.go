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
	// TypeKYCStepUpRequired fires when an unverified guest hits the
	// high-value booking gate (S140 — WF-GAP-019). The guest is nudged
	// to verify their identity so the next attempt at the same listing
	// can clear. Account-channel because identity verification is a
	// security-adjacent flow, not a per-booking event.
	TypeKYCStepUpRequired Type = "kyc_step_up_required"
	// TypeReviewSubmitted fires when a guest reviews a listing (the host
	// is notified) or a host reviews their guest (the guest is notified).
	// S148 — the in-app arm of the S136 ReviewSubmitted outbox event.
	// PushCatBookings because users think of reviews as part of the
	// post-stay booking flow, not an account-management chore.
	TypeReviewSubmitted Type = "review_submitted"
	// TypePaymentCaptured fires when the payment subscriber turns an
	// authorized hold into a real charge (typically when a booking is
	// confirmed). The guest sees a "your card was charged" prompt in
	// their inbox so the line item on their card statement isn't a
	// surprise. S157 — closes the dangling PaymentCaptured outbox
	// event introduced in S123: the type comment on
	// event.PaymentCaptured promised "downstream subscribers
	// (notifications, accounting)" that hadn't been wired until now.
	TypePaymentCaptured Type = "payment_captured"
	// TypePaymentRefunded fires when the payment subscriber refunds
	// some or all of an authorized or captured payment (typically
	// driven by a cancellation with a non-zero refund fraction). The
	// guest is told the refund is on its way so they can match the
	// credit on their card statement to the booking it came from. S157
	// — companion to TypePaymentCaptured, closing the second dangling
	// S123 event whose comments promised downstream subscribers that
	// didn't exist before this slice.
	TypePaymentRefunded Type = "payment_refunded"
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
