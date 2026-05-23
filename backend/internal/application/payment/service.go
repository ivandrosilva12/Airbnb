// Package paymentapp contains payment use cases. Money movement is driven by the
// booking lifecycle through domain events (authorize on request, capture on
// confirmation, refund on cancellation) and delegated to a PaymentGateway port.
package paymentapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates payment use cases.
type Service struct {
	repo       payment.Repository
	gateway    port.PaymentGateway
	bookings   booking.Repository
	properties property.Repository
}

// NewService wires the payment application service. The booking and property
// repositories are read-only dependencies used to build receipts.
func NewService(repo payment.Repository, gateway port.PaymentGateway, bookings booking.Repository, properties property.Repository) *Service {
	return &Service{repo: repo, gateway: gateway, bookings: bookings, properties: properties}
}

// GetForBooking returns the payment for a booking. Only the guest who owns it
// may view it (hosts/admins are handled elsewhere if needed).
func (s *Service) GetForBooking(ctx context.Context, actorID, bookingID uuid.UUID) (*payment.Payment, error) {
	p, err := s.repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if p.GuestID != actorID {
		return nil, shared.ErrForbidden
	}
	return p, nil
}

// ListForGuest returns a guest's payments.
func (s *Service) ListForGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*payment.Payment], error) {
	return s.repo.ListByGuest(ctx, guestID, page)
}

// ReceiptData is the read-model rendered into a payment receipt.
type ReceiptData struct {
	ReceiptNo     string
	IssuedAt      time.Time
	Status        payment.Status
	PropertyTitle string
	CheckIn       time.Time
	CheckOut      time.Time
	Nights        int
	Guests        int
	Subtotal      shared.Money
	CleaningFee   shared.Money
	ServiceFee    shared.Money
	Total         shared.Money
}

// Receipt assembles the data for a booking's payment receipt. Only the guest who
// owns the payment may obtain it.
func (s *Service) Receipt(ctx context.Context, actorID, bookingID uuid.UUID) (ReceiptData, error) {
	p, err := s.repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return ReceiptData{}, err
	}
	if p.GuestID != actorID {
		return ReceiptData{}, shared.ErrForbidden
	}
	b, err := s.bookings.FindByID(ctx, bookingID)
	if err != nil {
		return ReceiptData{}, err
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return ReceiptData{}, err
	}
	return ReceiptData{
		ReceiptNo:     p.ID.String(),
		IssuedAt:      time.Now().UTC(),
		Status:        p.Status,
		PropertyTitle: prop.Title,
		CheckIn:       b.Dates.CheckIn,
		CheckOut:      b.Dates.CheckOut,
		Nights:        b.Dates.Nights(),
		Guests:        b.Guests,
		Subtotal:      b.Pricing.Subtotal,
		CleaningFee:   b.Pricing.CleaningFee,
		ServiceFee:    b.Pricing.ServiceFee,
		Total:         b.Pricing.Total,
	}, nil
}
