// Package booking is the bounded context for reservations.
package booking

import (
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Status enumerates the reservation lifecycle.
type Status string

const (
	StatusPending   Status = "pending"
	StatusConfirmed Status = "confirmed"
	StatusCancelled Status = "cancelled"
	StatusCompleted Status = "completed"
)

// DateRange is a value object describing a check-in/check-out window. The range
// is half-open: [CheckIn, CheckOut), so a same-day turnover does not overlap.
type DateRange struct {
	CheckIn  time.Time
	CheckOut time.Time
}

// NewDateRange constructs a validated DateRange truncated to day precision.
func NewDateRange(checkIn, checkOut time.Time) (DateRange, error) {
	ci := truncateDay(checkIn)
	co := truncateDay(checkOut)
	if !co.After(ci) {
		return DateRange{}, shared.NewValidationError("check-out must be after check-in")
	}
	if ci.Before(truncateDay(time.Now().UTC())) {
		return DateRange{}, shared.NewValidationError("check-in cannot be in the past")
	}
	if int(co.Sub(ci).Hours()/24) > maxStayNights {
		return DateRange{}, shared.NewValidationError("a stay may not exceed 365 nights")
	}
	return DateRange{CheckIn: ci, CheckOut: co}, nil
}

// maxStayNights caps a single reservation length, guarding against absurd ranges
// (and any pricing overflow they would cause).
const maxStayNights = 365

// Nights returns the number of nights in the range.
func (d DateRange) Nights() int {
	return int(d.CheckOut.Sub(d.CheckIn).Hours() / 24)
}

// Overlaps reports whether two half-open ranges intersect.
func (d DateRange) Overlaps(other DateRange) bool {
	return d.CheckIn.Before(other.CheckOut) && other.CheckIn.Before(d.CheckOut)
}

// Booking is the aggregate root for a reservation.
type Booking struct {
	ID         uuid.UUID
	PropertyID uuid.UUID
	GuestID    uuid.UUID
	Dates      DateRange
	Guests     int
	Pricing    Pricing
	Status     Status
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// TotalPrice is the amount the guest pays — a convenience accessor over the
// pricing breakdown.
func (b *Booking) TotalPrice() shared.Money { return b.Pricing.Total }

// NewBooking creates a pending Booking. The full price breakdown (nightly
// subtotal, cleaning fee, platform service fee, total) is derived in the domain
// — clients never supply the price.
func NewBooking(
	propertyID, guestID uuid.UUID,
	dates DateRange,
	guests int,
	pricePerNight, cleaningFee shared.Money,
	serviceFeeRate float64,
	discounts Discounts,
) (*Booking, error) {
	if guests < 1 {
		return nil, shared.NewValidationError("at least one guest is required")
	}
	pricing, err := ComputePricing(pricePerNight, cleaningFee, dates.Nights(), serviceFeeRate, discounts)
	if err != nil {
		return nil, err
	}
	return newBookingWithPricing(propertyID, guestID, dates, guests, pricing), nil
}

// NewBookingFromSubtotal creates a pending Booking using a pre-computed
// accommodation subtotal — used when per-night prices vary across the stay
// (seasonal rules, weekend overrides). The caller is responsible for summing
// the per-night prices; the domain still derives discounts, fees, tax and the
// total from that subtotal.
func NewBookingFromSubtotal(
	propertyID, guestID uuid.UUID,
	dates DateRange,
	guests int,
	subtotal, cleaningFee shared.Money,
	serviceFeeRate float64,
	discounts Discounts,
) (*Booking, error) {
	if guests < 1 {
		return nil, shared.NewValidationError("at least one guest is required")
	}
	pricing, err := ComputePricingFromSubtotal(subtotal, cleaningFee, dates.Nights(), serviceFeeRate, discounts)
	if err != nil {
		return nil, err
	}
	return newBookingWithPricing(propertyID, guestID, dates, guests, pricing), nil
}

func newBookingWithPricing(propertyID, guestID uuid.UUID, dates DateRange, guests int, pricing Pricing) *Booking {
	now := time.Now().UTC()
	return &Booking{
		ID:         uuid.New(),
		PropertyID: propertyID,
		GuestID:    guestID,
		Dates:      dates,
		Guests:     guests,
		Pricing:    pricing,
		Status:     StatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// Reschedule changes the dates and/or guest count of a still-pending booking and
// recomputes its full price breakdown at the listing's current rates. Only a
// pending booking may be modified: once confirmed the payment has been captured,
// so a change would require a host-approved payment adjustment instead.
//
// Any promo code applied at creation is not carried over — the recomputed
// discount reflects only the listing's length-of-stay rules. Callers communicate
// this to the guest before they confirm the change.
func (b *Booking) Reschedule(
	dates DateRange,
	guests int,
	pricePerNight, cleaningFee shared.Money,
	serviceFeeRate float64,
	discounts Discounts,
) error {
	if err := b.guardReschedule(guests); err != nil {
		return err
	}
	pricing, err := ComputePricing(pricePerNight, cleaningFee, dates.Nights(), serviceFeeRate, discounts)
	if err != nil {
		return err
	}
	b.applyReschedule(dates, guests, pricing)
	return nil
}

// RescheduleFromSubtotal is the variable-per-night counterpart to Reschedule:
// it accepts a pre-summed accommodation subtotal so seasonal/weekend pricing
// rules can be honoured.
func (b *Booking) RescheduleFromSubtotal(
	dates DateRange,
	guests int,
	subtotal, cleaningFee shared.Money,
	serviceFeeRate float64,
	discounts Discounts,
) error {
	if err := b.guardReschedule(guests); err != nil {
		return err
	}
	pricing, err := ComputePricingFromSubtotal(subtotal, cleaningFee, dates.Nights(), serviceFeeRate, discounts)
	if err != nil {
		return err
	}
	b.applyReschedule(dates, guests, pricing)
	return nil
}

func (b *Booking) guardReschedule(guests int) error {
	if b.Status != StatusPending {
		return shared.NewValidationError("only a pending booking can be modified")
	}
	if guests < 1 {
		return shared.NewValidationError("at least one guest is required")
	}
	return nil
}

func (b *Booking) applyReschedule(dates DateRange, guests int, pricing Pricing) {
	b.Dates = dates
	b.Guests = guests
	b.Pricing = pricing
	b.touch()
}

// Confirm transitions a pending booking to confirmed.
func (b *Booking) Confirm() error {
	if b.Status != StatusPending {
		return shared.NewValidationError("only pending bookings can be confirmed")
	}
	b.Status = StatusConfirmed
	b.touch()
	return nil
}

// Cancel transitions an active booking to cancelled.
func (b *Booking) Cancel() error {
	if b.Status == StatusCompleted || b.Status == StatusCancelled {
		return shared.NewValidationError("booking can no longer be cancelled")
	}
	b.Status = StatusCancelled
	b.touch()
	return nil
}

// Complete marks a confirmed booking as finished after checkout.
func (b *Booking) Complete() error {
	if b.Status != StatusConfirmed {
		return shared.NewValidationError("only confirmed bookings can be completed")
	}
	b.Status = StatusCompleted
	b.touch()
	return nil
}

// IsActive reports whether the booking still occupies the calendar.
func (b *Booking) IsActive() bool {
	return b.Status == StatusPending || b.Status == StatusConfirmed
}

func (b *Booking) touch() { b.UpdatedAt = time.Now().UTC() }

func truncateDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
