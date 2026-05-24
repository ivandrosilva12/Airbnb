// Package payout is the bounded context for host earnings. A host accrues
// earnings when a booking is confirmed (the booking total minus the platform
// service fee) and is debited a refund entry when a booking is cancelled and
// money is returned to the guest. The running balance is the signed sum of the
// host's ledger entries.
package payout

import (
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Kind classifies a ledger entry.
type Kind string

const (
	// KindEarning credits the host with the net payout for a confirmed booking.
	KindEarning Kind = "earning"
	// KindRefund debits the host when a cancellation refunds the guest.
	KindRefund Kind = "refund"
)

// Entry is a single host-earnings ledger line tied to a booking. Amount is a
// non-negative magnitude; Kind determines whether it credits or debits the host.
type Entry struct {
	ID         uuid.UUID
	HostID     uuid.UUID
	BookingID  uuid.UUID
	PropertyID uuid.UUID
	Kind       Kind
	Amount     shared.Money
	CreatedAt  time.Time
}

func newEntry(kind Kind, hostID, bookingID, propertyID uuid.UUID, amount shared.Money) *Entry {
	return &Entry{
		ID:         uuid.New(),
		HostID:     hostID,
		BookingID:  bookingID,
		PropertyID: propertyID,
		Kind:       kind,
		Amount:     amount,
		CreatedAt:  time.Now().UTC(),
	}
}

// NewEarning records net earnings credited to a host for a confirmed booking.
func NewEarning(hostID, bookingID, propertyID uuid.UUID, amount shared.Money) *Entry {
	return newEntry(KindEarning, hostID, bookingID, propertyID, amount)
}

// NewRefund records a debit that reverses part or all of a prior earning.
func NewRefund(hostID, bookingID, propertyID uuid.UUID, amount shared.Money) *Entry {
	return newEntry(KindRefund, hostID, bookingID, propertyID, amount)
}

// SignedCents returns the entry's net impact on the host balance: positive for
// an earning, negative for a refund.
func (e *Entry) SignedCents() int64 {
	if e.Kind == KindRefund {
		return -e.Amount.AmountCents()
	}
	return e.Amount.AmountCents()
}

// Balance summarises a host's ledger within a single currency.
type Balance struct {
	Currency      string
	EarnedCents   int64
	RefundedCents int64
}

// NetCents is the host's lifetime net earnings (earnings minus refunds), before
// any payouts have been disbursed.
func (b Balance) NetCents() int64 { return b.EarnedCents - b.RefundedCents }

// DisbursementStatus tracks a payout's lifecycle: pending while the transfer is
// in flight, paid once the rail confirms it, failed if the rail rejected it.
type DisbursementStatus string

const (
	DisbursementPending DisbursementStatus = "pending"
	DisbursementPaid    DisbursementStatus = "paid"
	DisbursementFailed  DisbursementStatus = "failed"
)

// Disbursement is a payout of a host's available balance to their external
// account. The available balance is the net ledger minus amounts already
// committed to pending/paid disbursements, so funds cannot be paid out twice.
type Disbursement struct {
	ID         uuid.UUID
	HostID     uuid.UUID
	Currency   string
	AmountCents int64
	Status     DisbursementStatus
	GatewayRef string // reference returned by the payout rail (e.g. a Stripe transfer id)
	Failure    string // failure reason when Status is failed
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewDisbursement creates a pending payout for a positive amount.
func NewDisbursement(hostID uuid.UUID, currency string, amountCents int64) (*Disbursement, error) {
	if amountCents <= 0 {
		return nil, shared.NewValidationError("a payout amount must be positive")
	}
	now := time.Now().UTC()
	return &Disbursement{
		ID:          uuid.New(),
		HostID:      hostID,
		Currency:    currency,
		AmountCents: amountCents,
		Status:      DisbursementPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// MarkPaid records a successful payout with the rail's reference.
func (d *Disbursement) MarkPaid(ref string) {
	d.Status = DisbursementPaid
	d.GatewayRef = ref
	d.Failure = ""
	d.UpdatedAt = time.Now().UTC()
}

// MarkFailed records a rejected payout with a reason.
func (d *Disbursement) MarkFailed(reason string) {
	d.Status = DisbursementFailed
	d.Failure = reason
	d.UpdatedAt = time.Now().UTC()
}

// Committed reports whether the disbursement still ties up funds — pending and
// paid payouts both reduce the available balance; a failed one releases it.
func (d *Disbursement) Committed() bool {
	return d.Status == DisbursementPending || d.Status == DisbursementPaid
}
