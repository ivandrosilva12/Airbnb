// Package offerapp contains the use cases for host-initiated booking offers
// (pre-approvals and special offers). Accepting an offer creates a confirmed
// booking via the booking service, reusing its availability and pricing rules.
package offerapp

import (
	"context"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/offer"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates offer use cases.
type Service struct {
	offers     offer.Repository
	properties property.Repository
	bookings   *bookingapp.Service
}

// NewService wires the offer application service. It uses the booking service to
// turn an accepted offer into a confirmed reservation.
func NewService(offers offer.Repository, properties property.Repository, bookings *bookingapp.Service) *Service {
	return &Service{offers: offers, properties: properties, bookings: bookings}
}

// CreateInput carries a host's offer to a guest.
type CreateInput struct {
	HostID     uuid.UUID
	PropertyID uuid.UUID
	GuestID    uuid.UUID
	CheckIn    time.Time
	CheckOut   time.Time
	Guests     int
	// PriceCents is a nightly-price override; 0 = the listing's price (a
	// pre-approval), > 0 = a special offer.
	PriceCents int64
	Message    string
}

// Create sends an offer from a host to a guest. The host must own the listing,
// and the party size must fit its capacity.
func (s *Service) Create(ctx context.Context, in CreateInput) (*offer.Offer, error) {
	prop, err := s.properties.FindByID(ctx, in.PropertyID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(in.HostID) {
		return nil, shared.ErrForbidden
	}
	if in.Guests > prop.MaxGuests {
		return nil, shared.NewValidationError("number of guests exceeds property capacity")
	}
	o, err := offer.New(in.PropertyID, in.HostID, in.GuestID, in.CheckIn, in.CheckOut, in.Guests, in.PriceCents, prop.PricePerNight.Currency(), in.Message)
	if err != nil {
		return nil, err
	}
	if err := s.offers.Create(ctx, o); err != nil {
		return nil, err
	}
	return o, nil
}

// Accept turns a guest's pending offer into a confirmed booking: it creates the
// reservation (at the offer's dates, party size and any custom price) and
// confirms it on the host's behalf, since the host already approved.
func (s *Service) Accept(ctx context.Context, guestID, offerID uuid.UUID) (*booking.Booking, error) {
	o, err := s.offers.FindByID(ctx, offerID)
	if err != nil {
		return nil, err
	}
	if o.GuestID != guestID {
		return nil, shared.ErrForbidden
	}
	if err := o.Accept(); err != nil {
		// Persist an expiry transition so the stale offer doesn't linger as pending.
		if o.Status == offer.StatusExpired {
			_ = s.offers.Update(ctx, o)
		}
		return nil, err
	}
	b, err := s.bookings.Create(ctx, bookingapp.CreateInput{
		GuestID:                    guestID,
		PropertyID:                 o.PropertyID,
		CheckIn:                    o.CheckIn,
		CheckOut:                   o.CheckOut,
		Guests:                     o.Guests,
		OverridePricePerNightCents: o.PriceCents,
	})
	if err != nil {
		return nil, err
	}
	confirmed, err := s.bookings.Confirm(ctx, o.HostID, b.ID)
	if err != nil {
		return nil, err
	}
	if err := s.offers.Update(ctx, o); err != nil {
		return nil, err
	}
	return confirmed, nil
}

// Decline lets the guest turn down a pending offer.
func (s *Service) Decline(ctx context.Context, guestID, offerID uuid.UUID) error {
	o, err := s.offers.FindByID(ctx, offerID)
	if err != nil {
		return err
	}
	if o.GuestID != guestID {
		return shared.ErrForbidden
	}
	if err := o.Decline(); err != nil {
		return err
	}
	return s.offers.Update(ctx, o)
}

// Withdraw lets the host take back a pending offer.
func (s *Service) Withdraw(ctx context.Context, hostID, offerID uuid.UUID) error {
	o, err := s.offers.FindByID(ctx, offerID)
	if err != nil {
		return err
	}
	if o.HostID != hostID {
		return shared.ErrForbidden
	}
	if err := o.Withdraw(); err != nil {
		return err
	}
	return s.offers.Update(ctx, o)
}

// ListForGuest returns offers addressed to the guest.
func (s *Service) ListForGuest(ctx context.Context, guestID uuid.UUID) ([]*offer.Offer, error) {
	return s.offers.ListForGuest(ctx, guestID)
}

// ListForHost returns offers the host has sent.
func (s *Service) ListForHost(ctx context.Context, hostID uuid.UUID) ([]*offer.Offer, error) {
	return s.offers.ListForHost(ctx, hostID)
}
