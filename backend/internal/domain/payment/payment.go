// Package payment is the bounded context for booking payments. A Payment tracks
// the money owed for a booking through its lifecycle: it is authorized when the
// booking is requested, captured when the host confirms, and refunded if the
// booking is cancelled. The actual money movement is delegated to a Gateway
// port (implemented by a real or fake payment provider).
package payment

import (
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"time"
)

// Status enumerates the payment lifecycle.
type Status string

const (
	StatusPending    Status = "pending"
	StatusAuthorized Status = "authorized"
	StatusCaptured   Status = "captured"
	StatusRefunded   Status = "refunded"
	StatusFailed     Status = "failed"
)

// Payment is the aggregate root for a booking's payment.
type Payment struct {
	ID            uuid.UUID
	BookingID     uuid.UUID
	GuestID       uuid.UUID
	Amount        shared.Money
	Status        Status
	GatewayRef    string
	FailureReason string
	RefundedCents int64 // amount returned to the guest (0 unless refunded)
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// New creates a pending Payment for a booking.
func New(bookingID, guestID uuid.UUID, amount shared.Money) *Payment {
	now := time.Now().UTC()
	return &Payment{
		ID:        uuid.New(),
		BookingID: bookingID,
		GuestID:   guestID,
		Amount:    amount,
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Authorize records that funds have been held by the gateway.
func (p *Payment) Authorize(gatewayRef string) error {
	if p.Status != StatusPending {
		return shared.NewValidationError("only a pending payment can be authorized")
	}
	p.Status = StatusAuthorized
	p.GatewayRef = gatewayRef
	p.touch()
	return nil
}

// Reauthorize replaces an outstanding hold with a new one (a fresh gateway
// reference and amount) after the booking it pays for was modified. It is only
// valid while the payment is still authorized — nothing has been captured yet.
func (p *Payment) Reauthorize(gatewayRef string, amount shared.Money) error {
	if p.Status != StatusAuthorized {
		return shared.NewValidationError("only an authorized payment can be re-authorized")
	}
	p.GatewayRef = gatewayRef
	p.Amount = amount
	p.touch()
	return nil
}

// Capture charges previously authorized funds.
func (p *Payment) Capture() error {
	if p.Status != StatusAuthorized {
		return shared.NewValidationError("only an authorized payment can be captured")
	}
	p.Status = StatusCaptured
	p.touch()
	return nil
}

// Refund returns amountCents to the guest and marks the payment refunded.
// Allowed from authorized (releasing the hold) or captured (returning a charge).
// amountCents may be a partial amount (e.g. under a cancellation policy).
func (p *Payment) Refund(amountCents int64) error {
	if p.Status != StatusAuthorized && p.Status != StatusCaptured {
		return shared.NewValidationError("only an authorized or captured payment can be refunded")
	}
	if amountCents < 0 || amountCents > p.Amount.AmountCents() {
		return shared.NewValidationError("refund amount is out of range")
	}
	p.Status = StatusRefunded
	p.RefundedCents = amountCents
	p.touch()
	return nil
}

// Fail marks the payment as failed with a reason.
func (p *Payment) Fail(reason string) {
	p.Status = StatusFailed
	p.FailureReason = reason
	p.touch()
}

func (p *Payment) touch() { p.UpdatedAt = time.Now().UTC() }
