// Package offerapp contains the use cases for host-initiated booking offers
// (pre-approvals and special offers). Accepting an offer creates a confirmed
// booking via the booking service, reusing its availability and pricing rules.
package offerapp

import (
	"context"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/offer"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// OfferMetrics is the optional Prometheus sink for offer lifecycle events.
// It is defined here (rather than imported from the observability package)
// so the application layer does not depend on infrastructure: the wiring
// in main.go passes the concrete *observability.Metrics, which satisfies
// this interface via an inline adapter. eventName is the domain event's
// EventName() — one of "offer.created", "offer.declined",
// "offer.withdrawn" (S113 — follow-on to S99/WF-GAP-008).
type OfferMetrics interface {
	IncOffer(eventName string)
}

// Service orchestrates offer use cases.
type Service struct {
	offers     offer.Repository
	properties property.Repository
	bookings   *bookingapp.Service
	// pub, when wired via WithPublisher, receives OfferCreated/Declined/
	// Withdrawn events so subscribers (notification, email, realtime) can
	// inform the affected party. Optional — older tests / wiring leave it
	// nil and the emit helper is a silent no-op (S99 — WF-GAP-008).
	pub event.Publisher
	// metrics, when wired via WithMetrics, counts each published lifecycle
	// event by EventName so ops can graph the offer flow rate. Optional —
	// nil keeps observability off, matching the pre-S113 behaviour for
	// focused unit tests (S113).
	metrics OfferMetrics
}

// NewService wires the offer application service. It uses the booking service to
// turn an accepted offer into a confirmed reservation.
func NewService(offers offer.Repository, properties property.Repository, bookings *bookingapp.Service) *Service {
	return &Service{offers: offers, properties: properties, bookings: bookings}
}

// WithPublisher plugs an event Publisher into the service so lifecycle
// transitions are surfaced to subscribers (S99 — WF-GAP-008).
func (s *Service) WithPublisher(p event.Publisher) *Service {
	s.pub = p
	return s
}

// WithMetrics plugs in the Prometheus sink so every published offer
// lifecycle event increments the OffersTotal counter labeled by EventName
// (S113). Nil leaves observability off. Returns the receiver for fluent
// wiring — main.go calls .WithMetrics(metrics) right after .WithPublisher,
// mirroring fraud/experience-booking services.
func (s *Service) WithMetrics(m OfferMetrics) *Service {
	s.metrics = m
	return s
}

// emit is a small helper that publishes through s.pub when wired, otherwise
// silently drops — preserving the pre-S99 behaviour for tests that don't
// thread a publisher through. When a metrics sink is attached, it also
// increments the per-event counter so the count stays consistent with the
// dispatched events even if a subscriber later fails (S113).
func (s *Service) emit(ctx context.Context, e event.Event) {
	if s.pub != nil {
		s.pub.Publish(ctx, e)
	}
	if s.metrics != nil {
		s.metrics.IncOffer(e.EventName())
	}
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
	s.emit(ctx, event.OfferCreated{
		OfferID:       o.ID,
		PropertyID:    o.PropertyID,
		PropertyTitle: prop.Title,
		HostID:        o.HostID,
		GuestID:       o.GuestID,
		Kind:          offerKind(o),
	})
	return o, nil
}

// offerKind maps the domain offer to the event-side string the subscribers
// switch on. Kept here to avoid leaking domain implementation details.
func offerKind(o *offer.Offer) string {
	if o.PriceCents > 0 {
		return "special_offer"
	}
	return "pre_approval"
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
	if err := s.offers.Update(ctx, o); err != nil {
		return err
	}
	title := s.propertyTitle(ctx, o.PropertyID)
	s.emit(ctx, event.OfferDeclined{
		OfferID:       o.ID,
		PropertyID:    o.PropertyID,
		PropertyTitle: title,
		HostID:        o.HostID,
		GuestID:       o.GuestID,
	})
	return nil
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
	if err := s.offers.Update(ctx, o); err != nil {
		return err
	}
	title := s.propertyTitle(ctx, o.PropertyID)
	s.emit(ctx, event.OfferWithdrawn{
		OfferID:       o.ID,
		PropertyID:    o.PropertyID,
		PropertyTitle: title,
		HostID:        o.HostID,
		GuestID:       o.GuestID,
	})
	return nil
}

// propertyTitle resolves the listing's display title for the event payload
// at decline/withdraw time. Best-effort: if the listing is gone (a host
// deleted it after sending the offer), returns an empty string and the
// subscriber falls back to "your property" / "the property".
func (s *Service) propertyTitle(ctx context.Context, propertyID uuid.UUID) string {
	if prop, err := s.properties.FindByID(ctx, propertyID); err == nil && prop != nil {
		return prop.Title
	}
	return ""
}

// ListForGuest returns offers addressed to the guest.
func (s *Service) ListForGuest(ctx context.Context, guestID uuid.UUID) ([]*offer.Offer, error) {
	return s.offers.ListForGuest(ctx, guestID)
}

// ListForHost returns offers the host has sent.
func (s *Service) ListForHost(ctx context.Context, hostID uuid.UUID) ([]*offer.Offer, error) {
	return s.offers.ListForHost(ctx, hostID)
}
