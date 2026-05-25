// Package coupon is the bounded context for promotional discount codes a guest
// can apply to a booking. Coupons are platform-wide and created by an admin; a
// code reduces the accommodation subtotal either by a percentage or a fixed
// amount, optionally gated by a minimum stay, an expiry and a redemption cap.
package coupon

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Kind is how a coupon's discount is expressed.
type Kind string

const (
	// KindPercentage discounts a fraction of the accommodation subtotal.
	KindPercentage Kind = "percentage"
	// KindFixed discounts a fixed money amount (in the coupon's currency).
	KindFixed Kind = "fixed"
)

// Coupon is the aggregate root for a promotional code.
type Coupon struct {
	ID             uuid.UUID
	Code           string
	Kind           Kind
	Percent        float64 // 0 < p <= 1 for KindPercentage
	AmountCents    int64   // > 0 for KindFixed
	Currency       string  // required for KindFixed
	MinNights      int     // 0 = no minimum
	MaxRedemptions int     // 0 = unlimited
	Redemptions    int
	ExpiresAt      *time.Time
	Active         bool
	CreatedAt      time.Time
}

// NormalizeCode trims and upper-cases a code so lookups are case-insensitive.
func NormalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func validateCode(code string) (string, error) {
	code = NormalizeCode(code)
	if len(code) < 3 || len(code) > 32 {
		return "", shared.NewValidationError("coupon code must be 3–32 characters")
	}
	for _, r := range code {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return "", shared.NewValidationError("coupon code may only contain letters, digits, '-' and '_'")
		}
	}
	return code, nil
}

// NewPercentage creates a percentage coupon; percent is a fraction in (0,1].
func NewPercentage(code string, percent float64, minNights, maxRedemptions int, expiresAt *time.Time) (*Coupon, error) {
	code, err := validateCode(code)
	if err != nil {
		return nil, err
	}
	if percent <= 0 || percent > 1 {
		return nil, shared.NewValidationError("percentage must be between 0 and 1")
	}
	return newCoupon(code, KindPercentage, percent, 0, "", minNights, maxRedemptions, expiresAt)
}

// NewFixed creates a fixed-amount coupon denominated in currency.
func NewFixed(code string, amountCents int64, currency string, minNights, maxRedemptions int, expiresAt *time.Time) (*Coupon, error) {
	code, err := validateCode(code)
	if err != nil {
		return nil, err
	}
	if amountCents <= 0 {
		return nil, shared.NewValidationError("fixed amount must be positive")
	}
	if strings.TrimSpace(currency) == "" {
		return nil, shared.NewValidationError("a currency is required for a fixed coupon")
	}
	return newCoupon(code, KindFixed, 0, amountCents, strings.ToUpper(strings.TrimSpace(currency)), minNights, maxRedemptions, expiresAt)
}

func newCoupon(code string, kind Kind, percent float64, amountCents int64, currency string, minNights, maxRedemptions int, expiresAt *time.Time) (*Coupon, error) {
	if minNights < 0 {
		return nil, shared.NewValidationError("minimum nights cannot be negative")
	}
	if maxRedemptions < 0 {
		return nil, shared.NewValidationError("maximum redemptions cannot be negative")
	}
	if expiresAt != nil && !expiresAt.After(time.Now().UTC()) {
		return nil, shared.NewValidationError("expiry must be in the future")
	}
	return &Coupon{
		ID:             uuid.New(),
		Code:           code,
		Kind:           kind,
		Percent:        percent,
		AmountCents:    amountCents,
		Currency:       currency,
		MinNights:      minNights,
		MaxRedemptions: maxRedemptions,
		Active:         true,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

// redeemable reports whether the coupon can currently be applied.
func (c *Coupon) redeemable(now time.Time) error {
	if !c.Active {
		return shared.NewValidationError("this coupon is no longer active")
	}
	if c.ExpiresAt != nil && !now.Before(*c.ExpiresAt) {
		return shared.NewValidationError("this coupon has expired")
	}
	if c.MaxRedemptions > 0 && c.Redemptions >= c.MaxRedemptions {
		return shared.NewValidationError("this coupon has reached its redemption limit")
	}
	return nil
}

// DiscountFor returns the discount (in cents) this coupon yields on the given
// gross accommodation subtotal, validating applicability. The discount never
// exceeds the subtotal.
func (c *Coupon) DiscountFor(subtotalCents int64, currency string, nights int) (int64, error) {
	if err := c.redeemable(time.Now().UTC()); err != nil {
		return 0, err
	}
	if nights < c.MinNights {
		return 0, shared.NewValidationError(fmt.Sprintf("this coupon requires a stay of at least %d nights", c.MinNights))
	}
	var cents int64
	switch c.Kind {
	case KindPercentage:
		cents = int64(math.Round(float64(subtotalCents) * c.Percent))
	case KindFixed:
		if !strings.EqualFold(c.Currency, currency) {
			return 0, shared.NewValidationError("this coupon cannot be used in this currency")
		}
		cents = c.AmountCents
	default:
		return 0, shared.NewValidationError("unknown coupon type")
	}
	if cents > subtotalCents {
		cents = subtotalCents
	}
	if cents < 0 {
		cents = 0
	}
	return cents, nil
}

// Redeem records one use of the coupon, failing if it is no longer redeemable.
func (c *Coupon) Redeem() error {
	if err := c.redeemable(time.Now().UTC()); err != nil {
		return err
	}
	c.Redemptions++
	return nil
}

// Deactivate disables the coupon so it can no longer be applied.
func (c *Coupon) Deactivate() { c.Active = false }
