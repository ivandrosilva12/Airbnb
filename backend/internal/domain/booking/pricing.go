package booking

import (
	"math"

	"github.com/airhost/backend/internal/domain/shared"
)

// Nights thresholds at which length-of-stay discounts become available.
const (
	weeklyDiscountMinNights  = 7
	monthlyDiscountMinNights = 28
)

// Pricing is a value object describing the full cost breakdown of a stay:
// the nightly subtotal, any length-of-stay discount, the host's cleaning fee,
// the platform service fee, occupancy tax and the resulting total. Computing
// this in the domain keeps pricing rules in one place and out of the
// transport/UI layers.
type Pricing struct {
	Nights      int
	Subtotal    shared.Money // pricePerNight * nights (gross, before discount)
	Discount    shared.Money // length-of-stay discount deducted from the subtotal
	CleaningFee shared.Money
	ServiceFee  shared.Money // round((subtotal - discount + cleaning) * serviceFeeRate)
	Tax         shared.Money // round((subtotal - discount + cleaning) * taxRate)
	Total       shared.Money
}

// Discounts carries the length-of-stay discount and tax fractions a listing
// applies. All values are fractions in [0,1]; zero means no discount/tax.
type Discounts struct {
	WeeklyPct  float64
	MonthlyPct float64
	TaxPct     float64
}

// discountPctForNights picks the best qualifying length-of-stay discount.
func (d Discounts) discountPctForNights(nights int) float64 {
	if nights >= monthlyDiscountMinNights && d.MonthlyPct > 0 {
		return d.MonthlyPct
	}
	if nights >= weeklyDiscountMinNights && d.WeeklyPct > 0 {
		return d.WeeklyPct
	}
	return 0
}

// ComputePricing derives the cost breakdown for a stay. serviceFeeRate is a
// fraction (e.g. 0.12 for 12%). The cleaning fee must share the nightly price's
// currency. discounts applies optional length-of-stay discounts and tax.
func ComputePricing(pricePerNight, cleaningFee shared.Money, nights int, serviceFeeRate float64, discounts Discounts) (Pricing, error) {
	if nights < 1 {
		return Pricing{}, shared.NewValidationError("a stay must be at least one night")
	}
	if serviceFeeRate < 0 {
		return Pricing{}, shared.NewValidationError("service fee rate cannot be negative")
	}
	currency := pricePerNight.Currency()

	subtotal := pricePerNight.Mul(int64(nights))

	discountCents := int64(math.Round(float64(subtotal.AmountCents()) * discounts.discountPctForNights(nights)))
	discount, err := shared.NewMoney(discountCents, currency)
	if err != nil {
		return Pricing{}, err
	}

	// Accommodation net of discount, plus the cleaning fee, forms the base on
	// which the platform fee and tax are charged.
	baseCents := subtotal.AmountCents() - discountCents + cleaningFee.AmountCents()
	if cleaningFee.Currency() != currency {
		return Pricing{}, shared.NewValidationError("cleaning fee must use the same currency as the nightly price")
	}

	serviceCents := int64(math.Round(float64(baseCents) * serviceFeeRate))
	serviceFee, err := shared.NewMoney(serviceCents, currency)
	if err != nil {
		return Pricing{}, err
	}

	taxCents := int64(math.Round(float64(baseCents) * discounts.TaxPct))
	tax, err := shared.NewMoney(taxCents, currency)
	if err != nil {
		return Pricing{}, err
	}

	total, err := shared.NewMoney(baseCents+serviceCents+taxCents, currency)
	if err != nil {
		return Pricing{}, err
	}

	return Pricing{
		Nights:      nights,
		Subtotal:    subtotal,
		Discount:    discount,
		CleaningFee: cleaningFee,
		ServiceFee:  serviceFee,
		Tax:         tax,
		Total:       total,
	}, nil
}
