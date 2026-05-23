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

// Capture charges previously authorized funds.
func (p *Payment) Capture() error {
	if p.Status != StatusAuthorized {
		return shared.NewValidationError("only an authorized payment can be captured")
	}
	p.Status = StatusCaptured
	p.touch()
	return nil
}

// Refund returns funds to the guest. Allowed from authorized (releasing the
// hold) or captured (returning a charge).
func (p *Payment) Refund() error {
	if p.Status != StatusAuthorized && p.Status != StatusCaptured {
		return shared.NewValidationError("only an authorized or captured payment can be refunded")
	}
	p.Status = StatusRefunded
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
