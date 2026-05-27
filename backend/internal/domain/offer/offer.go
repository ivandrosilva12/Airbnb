// Package offer is the bounded context for host-initiated booking offers: a
// pre-approval (the host invites a guest to book at the listing price) or a
// special offer (a custom nightly price). The guest accepts an offer to create
// a confirmed booking, or declines it.
package offer

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Kind distinguishes a plain pre-approval from a custom-priced special offer.
type Kind string

const (
	KindPreApproval  Kind = "pre_approval"
	KindSpecialOffer Kind = "special_offer"
)

// Status is the lifecycle state of an offer.
type Status string

const (
	StatusPending   Status = "pending"
	StatusAccepted  Status = "accepted"
	StatusDeclined  Status = "declined"  // the guest turned it down
	StatusWithdrawn Status = "withdrawn" // the host took it back
	StatusExpired   Status = "expired"
)

// defaultValidity is how long an offer stays open before it expires.
const defaultValidity = 7 * 24 * time.Hour

// Offer is the aggregate root for a host's offer to a guest.
type Offer struct {
	ID         uuid.UUID
	PropertyID uuid.UUID
	HostID     uuid.UUID
	GuestID    uuid.UUID
	CheckIn    time.Time
	CheckOut   time.Time
	Guests     int
	// PriceCents is a nightly-price override; 0 means the listing's price
	// (a pre-approval). When > 0 the offer is a special offer.
	PriceCents int64
	Currency   string
	Message    string
	Kind       Kind
	Status     Status
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

func truncateDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// New creates a pending offer from a host to a guest, validating its dates,
// party size and (optional) custom price. A positive priceCents makes it a
// special offer; zero a pre-approval at the listing price.
func New(propertyID, hostID, guestID uuid.UUID, checkIn, checkOut time.Time, guests int, priceCents int64, currency, message string) (*Offer, error) {
	if hostID == guestID {
		return nil, shared.NewValidationError("a host cannot send an offer to themselves")
	}
	ci, co := truncateDay(checkIn), truncateDay(checkOut)
	if !co.After(ci) {
		return nil, shared.NewValidationError("check-out must be after check-in")
	}
	if ci.Before(truncateDay(time.Now().UTC())) {
		return nil, shared.NewValidationError("check-in cannot be in the past")
	}
	if guests < 1 {
		return nil, shared.NewValidationError("at least one guest is required")
	}
	if priceCents < 0 {
		return nil, shared.NewValidationError("price cannot be negative")
	}
	message = strings.TrimSpace(message)
	if len(message) > 1000 {
		return nil, shared.NewValidationError("message is too long")
	}
	kind := KindPreApproval
	if priceCents > 0 {
		kind = KindSpecialOffer
	}
	now := time.Now().UTC()
	return &Offer{
		ID:         uuid.New(),
		PropertyID: propertyID,
		HostID:     hostID,
		GuestID:    guestID,
		CheckIn:    ci,
		CheckOut:   co,
		Guests:     guests,
		PriceCents: priceCents,
		Currency:   currency,
		Message:    message,
		Kind:       kind,
		Status:     StatusPending,
		CreatedAt:  now,
		ExpiresAt:  now.Add(defaultValidity),
	}, nil
}

// IsOpen reports whether the offer can still be acted upon.
func (o *Offer) IsOpen(now time.Time) bool {
	return o.Status == StatusPending && !now.After(o.ExpiresAt)
}

// Accept marks the offer accepted (the guest is booking). Only an open offer
// can be accepted.
func (o *Offer) Accept() error {
	if o.Status != StatusPending {
		return shared.NewValidationError("only a pending offer can be accepted")
	}
	if time.Now().UTC().After(o.ExpiresAt) {
		o.Status = StatusExpired
		return shared.NewValidationError("this offer has expired")
	}
	o.Status = StatusAccepted
	return nil
}

// Decline marks the offer declined by the guest.
func (o *Offer) Decline() error {
	if o.Status != StatusPending {
		return shared.NewValidationError("only a pending offer can be declined")
	}
	o.Status = StatusDeclined
	return nil
}

// Withdraw marks the offer withdrawn by the host.
func (o *Offer) Withdraw() error {
	if o.Status != StatusPending {
		return shared.NewValidationError("only a pending offer can be withdrawn")
	}
	o.Status = StatusWithdrawn
	return nil
}
