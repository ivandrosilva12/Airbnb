// Package bookingapp contains reservation use cases. It coordinates the
// property and booking aggregates to enforce cross-aggregate rules such as
// no double-booking and host/guest authorization.
package bookingapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates booking use cases.
type Service struct {
	bookings   booking.Repository
	properties property.Repository
}

// NewService wires the booking application service.
func NewService(bookings booking.Repository, properties property.Repository) *Service {
	return &Service{bookings: bookings, properties: properties}
}

// CreateInput carries the data required to make a reservation.
type CreateInput struct {
	GuestID    uuid.UUID
	PropertyID uuid.UUID
	CheckIn    time.Time
	CheckOut   time.Time
	Guests     int
}

// Create makes a reservation, enforcing availability and capacity rules.
func (s *Service) Create(ctx context.Context, in CreateInput) (*booking.Booking, error) {
	prop, err := s.properties.FindByID(ctx, in.PropertyID)
	if err != nil {
		return nil, err
	}
	if err := prop.CanBeBookedBy(in.GuestID); err != nil {
		return nil, err
	}
	if in.Guests > prop.MaxGuests {
		return nil, shared.NewValidationError("number of guests exceeds property capacity")
	}

	dates, err := booking.NewDateRange(in.CheckIn, in.CheckOut)
	if err != nil {
		return nil, err
	}

	overlap, err := s.bookings.HasOverlap(ctx, in.PropertyID, dates)
	if err != nil {
		return nil, err
	}
	if overlap {
		return nil, shared.NewValidationError("selected dates are not available")
	}

	b, err := booking.NewBooking(in.PropertyID, in.GuestID, dates, in.Guests, prop.PricePerNight)
	if err != nil {
		return nil, err
	}
	if err := s.bookings.Create(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// GetByID fetches a reservation, ensuring the actor is a participant.
func (s *Service) GetByID(ctx context.Context, actorID, bookingID uuid.UUID) (*booking.Booking, error) {
	b, err := s.bookings.FindByID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if b.GuestID != actorID {
		// Hosts may also view bookings for their property.
		prop, err := s.properties.FindByID(ctx, b.PropertyID)
		if err != nil {
			return nil, err
		}
		if !prop.IsOwnedBy(actorID) {
			return nil, shared.ErrForbidden
		}
	}
	return b, nil
}

// ListForGuest returns a guest's reservations.
func (s *Service) ListForGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	return s.bookings.ListByGuest(ctx, guestID, page)
}

// ListForProperty returns reservations for a property the actor hosts.
func (s *Service) ListForProperty(ctx context.Context, actorID, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return shared.PageResult[*booking.Booking]{}, err
	}
	if !prop.IsOwnedBy(actorID) {
		return shared.PageResult[*booking.Booking]{}, shared.ErrForbidden
	}
	return s.bookings.ListByProperty(ctx, propertyID, page)
}

// Confirm confirms a pending booking; only the host may confirm.
func (s *Service) Confirm(ctx context.Context, actorID, bookingID uuid.UUID) (*booking.Booking, error) {
	b, prop, err := s.bookingWithProperty(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	if err := b.Confirm(); err != nil {
		return nil, err
	}
	if err := s.bookings.Update(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// Cancel cancels a booking; the guest or the host may cancel.
func (s *Service) Cancel(ctx context.Context, actorID, bookingID uuid.UUID) (*booking.Booking, error) {
	b, prop, err := s.bookingWithProperty(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if b.GuestID != actorID && !prop.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	if err := b.Cancel(); err != nil {
		return nil, err
	}
	if err := s.bookings.Update(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Service) bookingWithProperty(ctx context.Context, bookingID uuid.UUID) (*booking.Booking, *property.Property, error) {
	b, err := s.bookings.FindByID(ctx, bookingID)
	if err != nil {
		return nil, nil, err
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return nil, nil, err
	}
	return b, prop, nil
}
