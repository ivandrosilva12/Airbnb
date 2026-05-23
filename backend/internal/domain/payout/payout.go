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

// NetCents is the amount currently owed to the host (earnings minus refunds).
func (b Balance) NetCents() int64 { return b.EarnedCents - b.RefundedCents }
