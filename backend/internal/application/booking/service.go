// Package bookingapp contains reservation use cases. It coordinates the
// property and booking aggregates to enforce cross-aggregate rules such as
// no double-booking and host/guest authorization.
package bookingapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/block"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates booking use cases.
type Service struct {
	bookings       booking.Repository
	properties     property.Repository
	blocks         block.Repository
	serviceFeeRate float64
	events         event.Publisher
}

// NewService wires the booking application service. serviceFeeRate is the
// platform fee applied to each booking (e.g. 0.12 for 12%). publisher may be
// nil, in which case no domain events are emitted.
func NewService(bookings booking.Repository, properties property.Repository, blocks block.Repository, serviceFeeRate float64, publisher event.Publisher) *Service {
	if publisher == nil {
		publisher = event.Nop()
	}
	return &Service{bookings: bookings, properties: properties, blocks: blocks, serviceFeeRate: serviceFeeRate, events: publisher}
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

	blocked, err := s.blocks.HasOverlap(ctx, in.PropertyID, dates)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, shared.NewValidationError("selected dates are blocked by the host")
	}

	b, err := booking.NewBooking(in.PropertyID, in.GuestID, dates, in.Guests, prop.PricePerNight, prop.CleaningFee, s.serviceFeeRate, booking.Discounts{
		WeeklyPct:  prop.PricingPolicy.WeeklyDiscountPct,
		MonthlyPct: prop.PricingPolicy.MonthlyDiscountPct,
		TaxPct:     prop.PricingPolicy.TaxRatePct,
	})
	if err != nil {
		return nil, err
	}
	if err := s.bookings.Create(ctx, b); err != nil {
		return nil, err
	}
	s.events.Publish(ctx, event.BookingRequested{
		BookingID:     b.ID,
		PropertyID:    prop.ID,
		PropertyTitle: prop.Title,
		HostID:        prop.HostID,
		GuestID:       in.GuestID,
		TotalCents:    b.Pricing.Total.AmountCents(),
		Currency:      b.Pricing.Total.Currency(),
	})
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
	s.events.Publish(ctx, event.BookingConfirmed{
		BookingID:     b.ID,
		PropertyID:    prop.ID,
		PropertyTitle: prop.Title,
		GuestID:       b.GuestID,
	})
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

	// A host cancellation is the host's fault and always refunds in full;
	// otherwise the listing's cancellation policy decides the refund fraction.
	refundFraction := 1.0
	if !prop.IsOwnedBy(actorID) {
		today := time.Now().UTC().Truncate(24 * time.Hour)
		daysUntilCheckIn := int(b.Dates.CheckIn.Sub(today).Hours() / 24)
		refundFraction = prop.CancellationPolicy.RefundFraction(daysUntilCheckIn)
	}

	s.events.Publish(ctx, event.BookingCancelled{
		BookingID:      b.ID,
		PropertyID:     prop.ID,
		PropertyTitle:  prop.Title,
		HostID:         prop.HostID,
		GuestID:        b.GuestID,
		CancelledBy:    actorID,
		RefundFraction: refundFraction,
	})
	return b, nil
}

// Complete marks a confirmed booking as completed once the stay is over. Only
// the host may complete it, and only after the check-out date has passed —
// after which the guest becomes eligible to leave a review.
func (s *Service) Complete(ctx context.Context, actorID, bookingID uuid.UUID) (*booking.Booking, error) {
	b, prop, err := s.bookingWithProperty(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	if time.Now().UTC().Before(b.Dates.CheckOut) {
		return nil, shared.NewValidationError("a stay can only be completed after check-out")
	}
	if err := b.Complete(); err != nil {
		return nil, err
	}
	if err := s.bookings.Update(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// BookedRange is a read-model describing an occupied window of a property. The
// status is "blocked" for host blocks, otherwise the booking status.
type BookedRange struct {
	CheckIn  time.Time
	CheckOut time.Time
	Status   string
}

// Availability returns the occupied date ranges for a property within the
// [from, to) window — both active bookings and host blocks — so clients can show
// which dates are unavailable. This is a public read and intentionally exposes no
// guest-identifying data.
func (s *Service) Availability(ctx context.Context, propertyID uuid.UUID, from, to time.Time) ([]BookedRange, error) {
	if !to.After(from) {
		return nil, shared.NewValidationError("'to' must be after 'from'")
	}
	if _, err := s.properties.FindByID(ctx, propertyID); err != nil {
		return nil, err
	}
	active, err := s.bookings.ListActiveInRange(ctx, propertyID, from, to)
	if err != nil {
		return nil, err
	}
	blocks, err := s.blocks.ListInRange(ctx, propertyID, from, to)
	if err != nil {
		return nil, err
	}

	ranges := make([]BookedRange, 0, len(active)+len(blocks))
	for _, b := range active {
		ranges = append(ranges, BookedRange{CheckIn: b.Dates.CheckIn, CheckOut: b.Dates.CheckOut, Status: string(b.Status)})
	}
	for _, bl := range blocks {
		ranges = append(ranges, BookedRange{CheckIn: bl.Dates.CheckIn, CheckOut: bl.Dates.CheckOut, Status: "blocked"})
	}
	return ranges, nil
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
