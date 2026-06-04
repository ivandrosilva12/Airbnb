package event

import (
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/google/uuid"
)

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
	Register(DisputeOpened{}.EventName(), jsonDecoder[DisputeOpened]())
	Register(DisputeResolved{}.EventName(), jsonDecoder[DisputeResolved]())
	Register(SplitPaymentCompleted{}.EventName(), jsonDecoder[SplitPaymentCompleted]())
	// S87 — ExperienceBooking lifecycle events now flow through the outbox
	// (WF-GAP-020). They live in domain/experiencebooking (the BC owns its
	// own event types) but the decoder registration belongs alongside the
	// other property-booking decoders so a single place lists every event
	// the outbox knows how to re-hydrate.
	Register(experiencebooking.ExperienceBookingCreated{}.EventName(), jsonDecoder[experiencebooking.ExperienceBookingCreated]())
	Register(experiencebooking.ExperienceBookingConfirmed{}.EventName(), jsonDecoder[experiencebooking.ExperienceBookingConfirmed]())
	Register(experiencebooking.ExperienceBookingCancelled{}.EventName(), jsonDecoder[experiencebooking.ExperienceBookingCancelled]())
	// S88 — split-payment per-share lifecycle events (WF-GAP-001/005). One
	// SplitShareAuthorized per share when the gateway hold succeeds, one
	// SplitShareRefunded per share when a cancellation releases it.
	Register(SplitShareAuthorized{}.EventName(), jsonDecoder[SplitShareAuthorized]())
	Register(SplitShareRefunded{}.EventName(), jsonDecoder[SplitShareRefunded]())
	// S94 — async-gateway auto-confirm (WF-GAP-010): the payment service emits
	// this once the gateway settles an async authorization via webhook, and
	// the booking service subscriber promotes pending→confirmed when allowed.
	Register(PaymentAuthorized{}.EventName(), jsonDecoder[PaymentAuthorized]())
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
	// Split is true when the booking is being paid by a split-payment
	// plan (S20a). The payment subscriber ignores split bookings — money
	// flows through the splitpayment context instead, and the booking
	// confirms when every share is authorised.
	Split bool
}

func (BookingRequested) EventName() string { return "booking.requested" }

// BookingConfirmed is published when a host confirms a booking. The guest
// should be notified.
type BookingConfirmed struct {
	BookingID     uuid.UUID
	PropertyID    uuid.UUID
	PropertyTitle string
	GuestID       uuid.UUID
	// Split mirrors BookingRequested.Split: the payment subscriber skips
	// capture and deposit hold when it sees a split confirmation.
	Split bool
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
	// Split mirrors BookingRequested.Split: the payment subscriber skips
	// refund logic for split bookings; refunds (if any) flow through the
	// splitpayment context.
	Split bool
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

// DisputeOpened is published when a guest or host opens a Resolution Center
// case for a booking. The other party (the "respondent") is notified so they
// can respond, and admins see the new case in the moderation queue.
type DisputeOpened struct {
	DisputeID     uuid.UUID
	BookingID     uuid.UUID
	PropertyID    uuid.UUID
	PropertyTitle string
	HostID        uuid.UUID
	GuestID       uuid.UUID
	OpenerID      uuid.UUID
	Kind          string
}

func (DisputeOpened) EventName() string { return "dispute.opened" }

// DisputeResolved is published when a moderator decides a case (either siding
// with the opener or rejecting it). Both parties are notified of the outcome.
type DisputeResolved struct {
	DisputeID     uuid.UUID
	BookingID     uuid.UUID
	PropertyID    uuid.UUID
	PropertyTitle string
	HostID        uuid.UUID
	GuestID       uuid.UUID
	Outcome       string // "resolved" or "rejected"
	Resolution    string // public decision text
}

func (DisputeResolved) EventName() string { return "dispute.resolved" }

// SplitPaymentCompleted is published when every share of a split-payment
// plan has been authorised. A booking subscriber confirms the corresponding
// booking, which is otherwise stuck in pending while waiting for the team
// to finish paying.
type SplitPaymentCompleted struct {
	SplitPaymentID uuid.UUID
	BookingID      uuid.UUID
}

func (SplitPaymentCompleted) EventName() string { return "splitpayment.completed" }

// SplitShareAuthorized is published when a single share's gateway hold is
// successfully placed (WF-GAP-001 / S88). The reference is the per-share
// transaction id returned by the payment gateway and is what the cancel
// flow later passes to gateway.Refund to release the hold. Subscribers
// use this for analytics / audit; the booking confirmation event is
// SplitPaymentCompleted, fired separately once every share is paid.
type SplitShareAuthorized struct {
	SplitPaymentID uuid.UUID
	BookingID      uuid.UUID
	ShareID        uuid.UUID
	PayerID        uuid.UUID
	AmountCents    int64
	Currency       string
	GatewayRef     string
}

func (SplitShareAuthorized) EventName() string { return "splitpayment.share.authorized" }

// SplitShareRefunded is published when a previously-authorized share has
// its gateway hold released after a booking cancellation (WF-GAP-005 /
// S88). One event is emitted per refunded share so downstream consumers
// (analytics, the payer notification path) can react per contributor.
type SplitShareRefunded struct {
	SplitPaymentID uuid.UUID
	BookingID      uuid.UUID
	ShareID        uuid.UUID
	PayerID        uuid.UUID
	AmountCents    int64
	Currency       string
	GatewayRef     string
}

func (SplitShareRefunded) EventName() string { return "splitpayment.share.refunded" }

// PaymentAuthorized is published (S94 — WF-GAP-010) when an asynchronous
// gateway webhook completes the authorization of a payment that was
// previously in pending status (e.g. Stripe 3DS challenge cleared, AppyPay
// redirect completed). The booking context subscribes to this event so
// that an instant-book booking still in pending status can be auto-
// confirmed once the funds are actually held — closing the "stuck in
// pending" gap that exists when the initial Authorize call returns before
// the gateway has finished authenticating the payer.
//
// Subscribers must be idempotent: an at-least-once outbox plus the
// gateway's own webhook retries mean the same authorization can surface
// more than once. The booking subscriber guards against double-confirm
// by checking the booking is still in pending status before acting.
type PaymentAuthorized struct {
	BookingID  uuid.UUID
	PaymentID  uuid.UUID
	GuestID    uuid.UUID
	GatewayRef string
}

func (PaymentAuthorized) EventName() string { return "payment.authorized" }
