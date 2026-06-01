package disputeapp

import (
	"context"

	"github.com/google/uuid"
)

// PaymentAdjuster is the outbound port the dispute service calls when a
// moderator decision moves money. Implementations (paymentapp.Service) apply
// the actual payment-side effects and persist an audit row.
//
// disputeID is passed through as the (RefKind="dispute", RefID=disputeID)
// idempotency key so a retried/replayed call doesn't double-apply.
type PaymentAdjuster interface {
	// RefundForBooking returns amountCents to the guest of the given booking.
	// The implementation enforces that cumulative refunds never exceed the
	// captured amount.
	RefundForBooking(ctx context.Context, bookingID uuid.UUID, amountCents int64, reason string, disputeID uuid.UUID) error
	// DamageClaimForBooking records that the host is owed amountCents in
	// damage compensation tied to the booking. Audit only — no money moves at
	// the gateway from this call.
	DamageClaimForBooking(ctx context.Context, bookingID uuid.UUID, amountCents int64, reason string, disputeID uuid.UUID) error
}

// nopAdjuster is the default no-op used by tests/dev that don't wire payments
// into the dispute flow. It always reports success — the dispute lifecycle
// then proceeds with no payment side-effects.
type nopAdjuster struct{}

func (nopAdjuster) RefundForBooking(context.Context, uuid.UUID, int64, string, uuid.UUID) error {
	return nil
}
func (nopAdjuster) DamageClaimForBooking(context.Context, uuid.UUID, int64, string, uuid.UUID) error {
	return nil
}
