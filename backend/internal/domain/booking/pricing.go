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
// the nightly subtotal, any discount, the host's cleaning fee, the platform
// service fee, occupancy tax and the resulting total. Computing this in the
// domain keeps pricing rules in one place and out of the transport/UI layers.
type Pricing struct {
	Nights      int
	Subtotal    shared.Money // pricePerNight * nights (gross, before discount)
	Discount    shared.Money // total discount: length-of-stay plus any promo code
	CleaningFee shared.Money
	ServiceFee  shared.Money // round((subtotal - discount + cleaning) * serviceFeeRate)
	Tax         shared.Money // round((subtotal - discount + cleaning) * taxRate)
	Total       shared.Money
}

// Discounts carries the length-of-stay discount and tax fractions a listing
// applies, plus any absolute promo-code discount. The fractions are in [0,1];
// CouponCents is an absolute amount in the nightly price's currency.
type Discounts struct {
	WeeklyPct   float64
	MonthlyPct  float64
	TaxPct      float64
	CouponCents int64
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

	// Length-of-stay discount plus any promo-code discount, folded into a single
	// discount line and capped so it never exceeds the accommodation subtotal.
	losCents := int64(math.Round(float64(subtotal.AmountCents()) * discounts.discountPctForNights(nights)))
	couponCents := discounts.CouponCents
	if couponCents < 0 {
		couponCents = 0
	}
	discountCents := losCents + couponCents
	if discountCents > subtotal.AmountCents() {
		discountCents = subtotal.AmountCents()
	}
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
