package experiencebooking

import (
	"github.com/airhost/backend/internal/domain/shared"
)

// Pricing is the derived cost breakdown for an experience booking. The
// shape is intentionally simpler than booking.Pricing: per-guest
// activities have no cleaning fee, no nightly subtotal, no discounts
// ladder — just the seat price × guests plus the platform service fee.
//
// Tax + payout split land in later slices once the experience
// jurisdiction model is decided (an experience may run in a different
// country than the host's payout account).
type Pricing struct {
	PricePerGuest shared.Money
	Guests        int
	Subtotal      shared.Money // pricePerGuest * guests
	ServiceFee    shared.Money // subtotal * serviceFeeRate
	Total         shared.Money // subtotal + serviceFee
}

// ComputePricing derives the Pricing for guests×pricePerGuest with a
// platform service fee on top. serviceFeeRate is a fraction in [0, 1]
// — 0.10 means 10 %.
func ComputePricing(pricePerGuest shared.Money, guests int, serviceFeeRate float64) (Pricing, error) {
	if guests < 1 {
		return Pricing{}, shared.NewValidationError("experiencebooking: guests must be ≥ 1")
	}
	if pricePerGuest.AmountCents() < 0 {
		return Pricing{}, shared.NewValidationError("experiencebooking: pricePerGuest cannot be negative")
	}
	if serviceFeeRate < 0 || serviceFeeRate > 1 {
		return Pricing{}, shared.NewValidationError("experiencebooking: serviceFeeRate out of [0,1]")
	}
	subtotal := pricePerGuest.Mul(int64(guests))
	feeCents := int64(float64(subtotal.AmountCents()) * serviceFeeRate)
	fee, err := shared.NewMoney(feeCents, subtotal.Currency())
	if err != nil {
		return Pricing{}, err
	}
	total, err := subtotal.Add(fee)
	if err != nil {
		return Pricing{}, err
	}
	return Pricing{
		PricePerGuest: pricePerGuest,
		Guests:        guests,
		Subtotal:      subtotal,
		ServiceFee:    fee,
		Total:         total,
	}, nil
}
