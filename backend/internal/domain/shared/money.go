package shared

import "fmt"

// Money is a value object representing an amount in minor units (cents) with a
// currency. Storing minor units avoids floating-point rounding errors.
type Money struct {
	amountCents int64
	currency    string
}

// NewMoney constructs a Money value object, enforcing invariants.
func NewMoney(amountCents int64, currency string) (Money, error) {
	if amountCents < 0 {
		return Money{}, NewValidationError("money amount cannot be negative")
	}
	if len(currency) != 3 {
		return Money{}, NewValidationError("currency must be a 3-letter ISO code")
	}
	return Money{amountCents: amountCents, currency: currency}, nil
}

// AmountCents returns the amount in minor units.
func (m Money) AmountCents() int64 { return m.amountCents }

// Currency returns the ISO currency code.
func (m Money) Currency() string { return m.currency }

// Mul multiplies the amount by a non-negative integer factor (e.g. nights).
func (m Money) Mul(factor int64) Money {
	return Money{amountCents: m.amountCents * factor, currency: m.currency}
}

// Add returns the sum of two Money values of the same currency.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, NewValidationError("cannot add money of different currencies")
	}
	return Money{amountCents: m.amountCents + other.amountCents, currency: m.currency}, nil
}

// String renders the value as a human-readable string.
func (m Money) String() string {
	return fmt.Sprintf("%.2f %s", float64(m.amountCents)/100.0, m.currency)
}
