// Package payment is the bounded context for booking payments. A Payment tracks
// the money owed for a booking through its lifecycle: it is authorized when the
// booking is requested, captured when the host confirms, and refunded if the
// booking is cancelled. The actual money movement is delegated to a Gateway
// port (implemented by a real or fake payment provider).
package payment

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
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

// AdjustmentKind tells refund deductions apart from damage-claim credits in the
// payment_adjustments ledger.
type AdjustmentKind string

const (
	// AdjustmentRefund is money returned to the guest (post-stay partial refund,
	// dispute settlement). It is subtracted from the gross captured amount.
	AdjustmentRefund AdjustmentKind = "refund"
	// AdjustmentDamageClaim records compensation owed to the host for damage
	// caused during the stay. It does NOT move funds at the gateway by itself —
	// it is an accounting marker; actual charging is out of scope until a real
	// security-deposit hold exists.
	AdjustmentDamageClaim AdjustmentKind = "damage_claim"
)

// Adjustment is one entry in the payment's audit ledger — every partial refund
// or damage claim produced by a dispute resolution leaves a row here so the
// running totals on Payment can be reconstructed and explained.
//
// RefKind / RefID point at the external aggregate that justified the
// adjustment (e.g. RefKind="dispute", RefID=<dispute UUID>); the pair is used
// at storage level as an idempotency key so re-applying the same dispute
// outcome twice is a no-op.
type Adjustment struct {
	ID          uuid.UUID
	PaymentID   uuid.UUID
	Kind        AdjustmentKind
	AmountCents int64
	Reason      string
	RefKind     string
	RefID       uuid.UUID
	CreatedAt   time.Time
}

// Payment is the aggregate root for a booking's payment.
type Payment struct {
	ID               uuid.UUID
	BookingID        uuid.UUID
	GuestID          uuid.UUID
	Amount           shared.Money
	Status           Status
	GatewayRef       string
	FailureReason    string
	RefundedCents    int64 // cumulative amount returned to the guest (0 if none)
	DamageClaimCents int64 // cumulative damage owed to the host (audit only)
	Adjustments      []Adjustment
	CreatedAt        time.Time
	UpdatedAt        time.Time
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
//
// This is the one-shot policy refund used by the booking-cancelled flow; for
// the post-stay dispute flow that allows multiple cumulative partial refunds,
// use RefundPartial.
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

// RefundPartial records an additional partial refund against the payment and
// returns the Adjustment that captures it. Allowed from authorized (against
// the hold) or captured (against the charge). Multiple partial refunds may
// accumulate, but the running total can never exceed the original amount.
// The payment status only flips to Refunded when the cumulative refunds reach
// the full amount.
func (p *Payment) RefundPartial(amountCents int64, reason, refKind string, refID uuid.UUID) (Adjustment, error) {
	if p.Status != StatusAuthorized && p.Status != StatusCaptured {
		return Adjustment{}, shared.NewValidationError("only an authorized or captured payment can be refunded")
	}
	if amountCents <= 0 {
		return Adjustment{}, shared.NewValidationError("refund amount must be positive")
	}
	if p.RefundedCents+amountCents > p.Amount.AmountCents() {
		return Adjustment{}, shared.NewValidationError("refund exceeds remaining captured amount")
	}
	adj := Adjustment{
		ID:          uuid.New(),
		PaymentID:   p.ID,
		Kind:        AdjustmentRefund,
		AmountCents: amountCents,
		Reason:      strings.TrimSpace(reason),
		RefKind:     strings.TrimSpace(refKind),
		RefID:       refID,
		CreatedAt:   time.Now().UTC(),
	}
	p.RefundedCents += amountCents
	if p.RefundedCents == p.Amount.AmountCents() {
		p.Status = StatusRefunded
	}
	p.Adjustments = append(p.Adjustments, adj)
	p.touch()
	return adj, nil
}

// RecordDamageClaim notes that the moderator awarded the host amountCents in
// damage compensation tied to this payment. It is an accounting marker only —
// no money moves at the gateway from this call (real charging requires a
// dedicated security-deposit hold, which is a future slice). Multiple claims
// may accumulate.
func (p *Payment) RecordDamageClaim(amountCents int64, reason, refKind string, refID uuid.UUID) (Adjustment, error) {
	if amountCents <= 0 {
		return Adjustment{}, shared.NewValidationError("damage amount must be positive")
	}
	adj := Adjustment{
		ID:          uuid.New(),
		PaymentID:   p.ID,
		Kind:        AdjustmentDamageClaim,
		AmountCents: amountCents,
		Reason:      strings.TrimSpace(reason),
		RefKind:     strings.TrimSpace(refKind),
		RefID:       refID,
		CreatedAt:   time.Now().UTC(),
	}
	p.DamageClaimCents += amountCents
	p.Adjustments = append(p.Adjustments, adj)
	p.touch()
	return adj, nil
}

// HasAdjustmentFor reports whether the payment already carries an adjustment of
// the given kind sourced from the (refKind, refID) pair. Callers use this to
// guard against re-applying the same dispute outcome.
func (p *Payment) HasAdjustmentFor(kind AdjustmentKind, refKind string, refID uuid.UUID) bool {
	for _, a := range p.Adjustments {
		if a.Kind == kind && a.RefKind == refKind && a.RefID == refID {
			return true
		}
	}
	return false
}

// Fail marks the payment as failed with a reason.
func (p *Payment) Fail(reason string) {
	p.Status = StatusFailed
	p.FailureReason = reason
	p.touch()
}

func (p *Payment) touch() { p.UpdatedAt = time.Now().UTC() }
