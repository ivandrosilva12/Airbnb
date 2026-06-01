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
	Nights          int
	Subtotal        shared.Money // pricePerNight * nights (gross, before discount)
	Discount        shared.Money // total discount: length-of-stay plus any promo code
	CleaningFee     shared.Money
	ExtraGuestFee   shared.Money // per-extra-guest fee for the whole stay
	ServiceFee      shared.Money // round((subtotal - discount + cleaning + extraGuest) * serviceFeeRate)
	Tax             shared.Money // round((subtotal - discount + cleaning + extraGuest) * taxRate)
	SecurityDeposit shared.Money // refundable hold added to the total (no fee/tax)
	Total           shared.Money
}

// Discounts carries the pricing modifiers a listing applies: length-of-stay
// discount and tax fractions (in [0,1]), an absolute promo-code discount, the
// per-stay extra-guest fee, and a refundable security deposit — all absolute
// amounts in the nightly price's currency.
type Discounts struct {
	WeeklyPct            float64
	MonthlyPct           float64
	TaxPct               float64
	CouponCents          int64
	ExtraGuestFeeCents   int64
	SecurityDepositCents int64
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

// ComputePricing derives the cost breakdown for a stay at a uniform nightly
// price. It is a thin wrapper over ComputePricingFromSubtotal that multiplies
// the price by the number of nights to form the subtotal.
func ComputePricing(pricePerNight, cleaningFee shared.Money, nights int, serviceFeeRate float64, discounts Discounts) (Pricing, error) {
	if nights < 1 {
		return Pricing{}, shared.NewValidationError("a stay must be at least one night")
	}
	return ComputePricingFromSubtotal(pricePerNight.Mul(int64(nights)), cleaningFee, nights, serviceFeeRate, discounts)
}

// ComputePricingFromSubtotal derives the cost breakdown given an already-computed
// nightly subtotal — the path used when per-night prices vary (seasonal rules
// or a weekend override). cleaningFee must share the subtotal's currency.
func ComputePricingFromSubtotal(subtotal, cleaningFee shared.Money, nights int, serviceFeeRate float64, discounts Discounts) (Pricing, error) {
	if nights < 1 {
		return Pricing{}, shared.NewValidationError("a stay must be at least one night")
	}
	if serviceFeeRate < 0 {
		return Pricing{}, shared.NewValidationError("service fee rate cannot be negative")
	}
	currency := subtotal.Currency()

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

	if cleaningFee.Currency() != currency {
		return Pricing{}, shared.NewValidationError("cleaning fee must use the same currency as the nightly price")
	}
	extraGuestCents := discounts.ExtraGuestFeeCents
	if extraGuestCents < 0 {
		extraGuestCents = 0
	}
	depositCents := discounts.SecurityDepositCents
	if depositCents < 0 {
		depositCents = 0
	}
	extraGuestFee, err := shared.NewMoney(extraGuestCents, currency)
	if err != nil {
		return Pricing{}, err
	}
	deposit, err := shared.NewMoney(depositCents, currency)
	if err != nil {
		return Pricing{}, err
	}

	// Accommodation net of discount, plus the cleaning and extra-guest fees, forms
	// the base on which the platform fee and tax are charged.
	baseCents := subtotal.AmountCents() - discountCents + cleaningFee.AmountCents() + extraGuestCents

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

	// The refundable deposit is collected on top (no platform fee or tax on it).
	total, err := shared.NewMoney(baseCents+serviceCents+taxCents+depositCents, currency)
	if err != nil {
		return Pricing{}, err
	}

	return Pricing{
		Nights:          nights,
		Subtotal:        subtotal,
		Discount:        discount,
		CleaningFee:     cleaningFee,
		ExtraGuestFee:   extraGuestFee,
		ServiceFee:      serviceFee,
		Tax:             tax,
		SecurityDeposit: deposit,
		Total:           total,
	}, nil
}
