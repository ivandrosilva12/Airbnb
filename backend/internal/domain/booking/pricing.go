package booking

import (
	"math"

	"github.com/airhost/backend/internal/domain/shared"
)

// Pricing is a value object describing the full cost breakdown of a stay:
// the nightly subtotal, the host's cleaning fee, the platform service fee and
// the resulting total. Computing this in the domain keeps pricing rules in one
// place and out of the transport/UI layers.
type Pricing struct {
	Nights      int
	Subtotal    shared.Money // pricePerNight * nights
	CleaningFee shared.Money
	ServiceFee  shared.Money // round((subtotal + cleaning) * serviceFeeRate)
	Total       shared.Money
}

// ComputePricing derives the cost breakdown for a stay. serviceFeeRate is a
// fraction (e.g. 0.12 for 12%). The cleaning fee must share the nightly price's
// currency.
func ComputePricing(pricePerNight, cleaningFee shared.Money, nights int, serviceFeeRate float64) (Pricing, error) {
	if nights < 1 {
		return Pricing{}, shared.NewValidationError("a stay must be at least one night")
	}
	if serviceFeeRate < 0 {
		return Pricing{}, shared.NewValidationError("service fee rate cannot be negative")
	}

	subtotal := pricePerNight.Mul(int64(nights))
	base, err := subtotal.Add(cleaningFee) // also enforces matching currency
	if err != nil {
		return Pricing{}, err
	}

	serviceCents := int64(math.Round(float64(base.AmountCents()) * serviceFeeRate))
	serviceFee, err := shared.NewMoney(serviceCents, pricePerNight.Currency())
	if err != nil {
		return Pricing{}, err
	}
	total, err := base.Add(serviceFee)
	if err != nil {
		return Pricing{}, err
	}

	return Pricing{
		Nights:      nights,
		Subtotal:    subtotal,
		CleaningFee: cleaningFee,
		ServiceFee:  serviceFee,
		Total:       total,
	}, nil
}
