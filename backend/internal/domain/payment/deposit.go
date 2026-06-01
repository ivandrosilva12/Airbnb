package payment

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// DepositStatus enumerates the lifecycle of a security-deposit hold. It is
// kept separate from the booking's Payment because deposits flow differently
// (held → released after a clean stay, or partially captured to satisfy a
// host's damage claim — funds the platform credits the host out-of-band, not
// part of the rental settlement).
type DepositStatus string

const (
	DepositPending            DepositStatus = "pending"             // not yet placed at the gateway
	DepositAuthorized         DepositStatus = "authorized"          // hold placed, nothing claimed
	DepositPartiallyCaptured  DepositStatus = "partially_captured"  // some captured, remainder still held
	DepositCaptured           DepositStatus = "captured"            // fully captured (no remainder)
	DepositReleased           DepositStatus = "released"            // remainder explicitly returned to the guest
	DepositFailed             DepositStatus = "failed"              // the gateway refused the hold
)

// IsTerminal reports whether the deposit has reached an end state (no more
// money will move).
func (s DepositStatus) IsTerminal() bool {
	return s == DepositCaptured || s == DepositReleased || s == DepositFailed
}

// DepositHold is the aggregate for a refundable security deposit attached to a
// booking. The total Amount is the cap; CapturedCents is the running tally of
// money already moved to the host as compensation for damage. Released marks
// that the remaining hold has been returned to the guest.
//
// Capture entries are appended to the same payment Adjustment ledger used by
// post-stay refunds (kind = AdjustmentDepositCapture), so the audit trail for
// a booking lives in one place.
type DepositHold struct {
	ID            uuid.UUID
	BookingID     uuid.UUID
	GuestID       uuid.UUID
	Amount        shared.Money
	CapturedCents int64
	Status        DepositStatus
	GatewayRef    string
	FailureReason string
	Adjustments   []Adjustment
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ReleasedAt    *time.Time
}

// NewDepositHold creates a pending DepositHold. Once the gateway accepts the
// authorization the caller should invoke Authorize(ref); if it refuses,
// Fail(reason).
func NewDepositHold(bookingID, guestID uuid.UUID, amount shared.Money) (*DepositHold, error) {
	if bookingID == uuid.Nil {
		return nil, shared.NewValidationError("booking is required")
	}
	if guestID == uuid.Nil {
		return nil, shared.NewValidationError("guest is required")
	}
	if amount.AmountCents() <= 0 {
		return nil, shared.NewValidationError("deposit amount must be positive")
	}
	now := time.Now().UTC()
	return &DepositHold{
		ID:        uuid.New(),
		BookingID: bookingID,
		GuestID:   guestID,
		Amount:    amount,
		Status:    DepositPending,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Authorize records that the gateway accepted the hold.
func (d *DepositHold) Authorize(gatewayRef string) error {
	if d.Status != DepositPending {
		return shared.NewValidationError("only a pending deposit can be authorized")
	}
	d.GatewayRef = gatewayRef
	d.Status = DepositAuthorized
	d.touch()
	return nil
}

// Fail marks the hold as failed with a reason (the gateway refused the
// authorization).
func (d *DepositHold) Fail(reason string) {
	d.Status = DepositFailed
	d.FailureReason = reason
	d.touch()
}

// Remaining returns how much of the hold can still be captured or released.
func (d *DepositHold) Remaining() int64 {
	return d.Amount.AmountCents() - d.CapturedCents
}

// CapturePartial moves amountCents from the deposit to the host as
// compensation for damage. The amount is capped at the remaining hold so
// callers can pass the full damage claim and let the aggregate decide how
// much fits. Returns the Adjustment recorded plus the captured amount (the
// caller subtracts it from any overflow it needs to record elsewhere).
//
// If the hold has no remaining capacity, the call is a no-op and returns
// (zero Adjustment, 0).
func (d *DepositHold) CapturePartial(amountCents int64, reason, refKind string, refID uuid.UUID) (Adjustment, int64, error) {
	if d.Status != DepositAuthorized && d.Status != DepositPartiallyCaptured {
		return Adjustment{}, 0, shared.NewValidationError("deposit is not active")
	}
	if amountCents <= 0 {
		return Adjustment{}, 0, shared.NewValidationError("capture amount must be positive")
	}
	take := amountCents
	if remaining := d.Remaining(); take > remaining {
		take = remaining
	}
	if take == 0 {
		return Adjustment{}, 0, nil
	}
	adj := Adjustment{
		ID:          uuid.New(),
		PaymentID:   d.ID,
		Kind:        AdjustmentDepositCapture,
		AmountCents: take,
		Reason:      strings.TrimSpace(reason),
		RefKind:     strings.TrimSpace(refKind),
		RefID:       refID,
		CreatedAt:   time.Now().UTC(),
	}
	d.CapturedCents += take
	if d.CapturedCents == d.Amount.AmountCents() {
		d.Status = DepositCaptured
	} else {
		d.Status = DepositPartiallyCaptured
	}
	d.Adjustments = append(d.Adjustments, adj)
	d.touch()
	return adj, take, nil
}

// Release returns the remaining hold to the guest and marks the deposit
// closed. Idempotent on already-released deposits; rejects releases on a
// fully-captured deposit (use a fresh hold for the next stay).
func (d *DepositHold) Release() error {
	switch d.Status {
	case DepositReleased:
		return nil
	case DepositCaptured, DepositFailed:
		return shared.NewValidationError("deposit is already closed")
	case DepositPending:
		return shared.NewValidationError("a pending deposit cannot be released")
	}
	now := time.Now().UTC()
	d.Status = DepositReleased
	d.ReleasedAt = &now
	d.touch()
	return nil
}

// HasAdjustmentFor reports whether a capture from the (refKind, refID) source
// has already been recorded — used to keep dispute-driven captures idempotent.
func (d *DepositHold) HasAdjustmentFor(refKind string, refID uuid.UUID) bool {
	for _, a := range d.Adjustments {
		if a.Kind == AdjustmentDepositCapture && a.RefKind == refKind && a.RefID == refID {
			return true
		}
	}
	return false
}

func (d *DepositHold) touch() { d.UpdatedAt = time.Now().UTC() }
