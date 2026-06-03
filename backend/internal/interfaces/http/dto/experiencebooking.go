package dto

import (
	"time"

	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/google/uuid"
)

// ExperienceBookingPricingView is the wire shape for the derived
// booking pricing breakdown.
type ExperienceBookingPricingView struct {
	PricePerGuest MoneyView `json:"pricePerGuest"`
	Guests        int       `json:"guests"`
	Subtotal      MoneyView `json:"subtotal"`
	ServiceFee    MoneyView `json:"serviceFee"`
	Total         MoneyView `json:"total"`
}

// ExperienceBookingView is the wire shape for one booking.
type ExperienceBookingView struct {
	ID           uuid.UUID                    `json:"id"`
	ExperienceID uuid.UUID                    `json:"experienceId"`
	HostID       uuid.UUID                    `json:"hostId"`
	GuestID      uuid.UUID                    `json:"guestId"`
	StartAt      time.Time                    `json:"startAt"`
	EndAt        time.Time                    `json:"endAt"`
	Guests       int                          `json:"guests"`
	Status       string                       `json:"status"`
	Pricing      ExperienceBookingPricingView `json:"pricing"`
	CreatedAt    time.Time                    `json:"createdAt"`
	UpdatedAt    time.Time                    `json:"updatedAt"`
}

// FromExperienceBooking maps a domain Booking to the wire shape.
func FromExperienceBooking(b *experiencebooking.Booking) ExperienceBookingView {
	if b == nil {
		return ExperienceBookingView{}
	}
	return ExperienceBookingView{
		ID:           b.ID,
		ExperienceID: b.ExperienceID,
		HostID:       b.HostID,
		GuestID:      b.GuestID,
		StartAt:      b.Session.StartAt,
		EndAt:        b.Session.EndAt(),
		Guests:       b.Guests,
		Status:       string(b.Status),
		Pricing: ExperienceBookingPricingView{
			PricePerGuest: fromMoney(b.Pricing.PricePerGuest),
			Guests:        b.Pricing.Guests,
			Subtotal:      fromMoney(b.Pricing.Subtotal),
			ServiceFee:    fromMoney(b.Pricing.ServiceFee),
			Total:         fromMoney(b.Pricing.Total),
		},
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
	}
}
