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
	return DateRange{CheckIn: ci, CheckOut: co}, nil
}

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
	TotalPrice shared.Money
	Status     Status
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewBooking creates a pending Booking. Total price is derived from the
// per-night price and number of nights — pricing belongs to the domain.
func NewBooking(
	propertyID, guestID uuid.UUID,
	dates DateRange,
	guests int,
	pricePerNight shared.Money,
) (*Booking, error) {
	if guests < 1 {
		return nil, shared.NewValidationError("at least one guest is required")
	}
	total := pricePerNight.Mul(int64(dates.Nights()))

	now := time.Now().UTC()
	return &Booking{
		ID:         uuid.New(),
		PropertyID: propertyID,
		GuestID:    guestID,
		Dates:      dates,
		Guests:     guests,
		TotalPrice: total,
		Status:     StatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
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
