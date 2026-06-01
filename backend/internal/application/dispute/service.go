// Package disputeapp contains Resolution Center use cases: a guest or host
// opens a post-stay case, either side adds evidence over time, and an admin
// decides the outcome. Domain events DisputeOpened and DisputeResolved are
// published so the notification / email / push pipelines pick them up.
//
// Money movement triggered by a resolved dispute (partial refund, damage
// debit) is intentionally not handled here — S14 will plug it in once this
// lifecycle is stable.
package disputeapp

import (
	"context"
	"errors"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/dispute"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// PostStayWindow is the number of days after a booking is completed or
// cancelled during which a dispute can still be opened. Beyond that the
// platform considers the matter closed.
const PostStayWindow = 14 * 24 * time.Hour

// Service orchestrates dispute use cases.
type Service struct {
	repo       dispute.Repository
	bookings   booking.Repository
	properties property.Repository
	pub        event.Publisher
}

// NewService wires the dispute application service. When pub is nil events are
// silently dropped (useful in some tests).
func NewService(repo dispute.Repository, bookings booking.Repository, properties property.Repository, pub event.Publisher) *Service {
	if pub == nil {
		pub = event.Nop()
	}
	return &Service{repo: repo, bookings: bookings, properties: properties, pub: pub}
}

// OpenInput carries the inputs for filing a new dispute.
type OpenInput struct {
	BookingID            uuid.UUID
	OpenerID             uuid.UUID
	Kind                 dispute.Kind
	Reason               string
	RequestedAmountCents int64
	Currency             string
}

// Open files a new dispute against a booking. The opener must be the guest or
// host of the booking; the booking must be completed or cancelled; and the
// post-stay window must still be open. At most one active dispute per booking
// is allowed.
func (s *Service) Open(ctx context.Context, in OpenInput) (*dispute.Dispute, error) {
	b, err := s.bookings.FindByID(ctx, in.BookingID)
	if err != nil {
		return nil, err
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return nil, err
	}
	if in.OpenerID != b.GuestID && in.OpenerID != prop.HostID {
		return nil, shared.NewValidationError("only the booking's guest or host can open a case")
	}
	if b.Status != booking.StatusCompleted && b.Status != booking.StatusCancelled {
		return nil, shared.NewValidationError("the stay must be completed or cancelled before opening a case")
	}
	// The post-stay window is anchored at check-out, or at the booking's
	// updated_at when it was cancelled before check-in (so a stay cancelled
	// months in advance still gets the 14-day window from the cancellation).
	anchor := b.Dates.CheckOut
	if b.Status == booking.StatusCancelled || b.UpdatedAt.After(anchor) {
		anchor = b.UpdatedAt
	}
	if time.Since(anchor) > PostStayWindow {
		return nil, shared.NewValidationError("the post-stay window for opening a case has closed")
	}
	if existing, err := s.repo.FindActiveByBooking(ctx, in.BookingID); err == nil && existing != nil {
		return nil, shared.NewValidationError("an active case already exists for this booking")
	} else if err != nil && !errors.Is(err, shared.ErrNotFound) {
		return nil, err
	}
	currency := in.Currency
	if currency == "" {
		currency = b.Pricing.Total.Currency()
	}
	d, err := dispute.New(in.BookingID, in.OpenerID, in.Kind, in.Reason, in.RequestedAmountCents, currency)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, d); err != nil {
		return nil, err
	}
	s.pub.Publish(ctx, event.DisputeOpened{
		DisputeID:     d.ID,
		BookingID:     b.ID,
		PropertyID:    prop.ID,
		PropertyTitle: prop.Title,
		HostID:        prop.HostID,
		GuestID:       b.GuestID,
		OpenerID:      in.OpenerID,
		Kind:          string(d.Kind),
	})
	return d, nil
}

// AddEvidence appends a note / URL to a dispute. Either party of the booking,
// or the moderator who claimed the case, may contribute.
func (s *Service) AddEvidence(ctx context.Context, actorID, disputeID uuid.UUID, url, note string) (*dispute.Dispute, error) {
	d, err := s.repo.FindByID(ctx, disputeID)
	if err != nil {
		return nil, err
	}
	if !s.canParticipate(ctx, d, actorID) {
		return nil, shared.ErrForbidden
	}
	if _, err := d.AddEvidence(actorID, url, note); err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// HostRespond records the host's side. Only the host of the booking's listing
// is allowed; the host might or might not be the original opener.
func (s *Service) HostRespond(ctx context.Context, hostID, disputeID uuid.UUID, response string) (*dispute.Dispute, error) {
	d, err := s.repo.FindByID(ctx, disputeID)
	if err != nil {
		return nil, err
	}
	b, err := s.bookings.FindByID(ctx, d.BookingID)
	if err != nil {
		return nil, err
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return nil, err
	}
	if prop.HostID != hostID {
		return nil, shared.ErrForbidden
	}
	if err := d.HostRespond(response); err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// ListMine returns disputes a user opened plus disputes filed against any of
// their properties (host respondent view). Newest-first; deduped by id.
func (s *Service) ListMine(ctx context.Context, userID uuid.UUID, page shared.Page) ([]*dispute.Dispute, error) {
	opened, err := s.repo.ListByOpener(ctx, userID, page)
	if err != nil {
		return nil, err
	}
	props, err := s.properties.ListByHost(ctx, userID, shared.Page{Limit: 200})
	if err != nil {
		return nil, err
	}
	var bookingIDs []uuid.UUID
	for _, p := range props.Items {
		bs, err := s.bookings.ListByProperty(ctx, p.ID, shared.Page{Limit: 500})
		if err != nil {
			return nil, err
		}
		for _, b := range bs.Items {
			bookingIDs = append(bookingIDs, b.ID)
		}
	}
	var asHost shared.PageResult[*dispute.Dispute]
	if len(bookingIDs) > 0 {
		asHost, err = s.repo.ListByBookings(ctx, bookingIDs, shared.Page{Limit: 200})
		if err != nil {
			return nil, err
		}
	}
	seen := map[uuid.UUID]bool{}
	out := make([]*dispute.Dispute, 0, len(opened.Items)+len(asHost.Items))
	for _, d := range opened.Items {
		if !seen[d.ID] {
			out = append(out, d)
			seen[d.ID] = true
		}
	}
	for _, d := range asHost.Items {
		if !seen[d.ID] {
			out = append(out, d)
			seen[d.ID] = true
		}
	}
	return out, nil
}

// ListOpen returns the moderation queue.
func (s *Service) ListOpen(ctx context.Context, page shared.Page) (shared.PageResult[*dispute.Dispute], error) {
	return s.repo.ListOpen(ctx, page)
}

// GetByID returns a dispute the actor is allowed to see.
func (s *Service) GetByID(ctx context.Context, actorID, id uuid.UUID) (*dispute.Dispute, error) {
	d, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.canParticipate(ctx, d, actorID) {
		return nil, shared.ErrForbidden
	}
	return d, nil
}

// AdminResolve closes the dispute siding with the opener.
func (s *Service) AdminResolve(ctx context.Context, adminID, disputeID uuid.UUID, resolution string) (*dispute.Dispute, error) {
	return s.adminDecide(ctx, adminID, disputeID, resolution, true)
}

// AdminReject closes the dispute siding against the opener.
func (s *Service) AdminReject(ctx context.Context, adminID, disputeID uuid.UUID, resolution string) (*dispute.Dispute, error) {
	return s.adminDecide(ctx, adminID, disputeID, resolution, false)
}

func (s *Service) adminDecide(ctx context.Context, adminID, disputeID uuid.UUID, resolution string, sideWithOpener bool) (*dispute.Dispute, error) {
	d, err := s.repo.FindByID(ctx, disputeID)
	if err != nil {
		return nil, err
	}
	b, err := s.bookings.FindByID(ctx, d.BookingID)
	if err != nil {
		return nil, err
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return nil, err
	}
	if sideWithOpener {
		if err := d.AdminResolve(adminID, resolution); err != nil {
			return nil, err
		}
	} else {
		if err := d.AdminReject(adminID, resolution); err != nil {
			return nil, err
		}
	}
	if err := s.repo.Save(ctx, d); err != nil {
		return nil, err
	}
	outcome := "resolved"
	if !sideWithOpener {
		outcome = "rejected"
	}
	s.pub.Publish(ctx, event.DisputeResolved{
		DisputeID:     d.ID,
		BookingID:     b.ID,
		PropertyID:    prop.ID,
		PropertyTitle: prop.Title,
		HostID:        prop.HostID,
		GuestID:       b.GuestID,
		Outcome:       outcome,
		Resolution:    d.Resolution,
	})
	return d, nil
}

// canParticipate reports whether actor is the dispute opener, the booking's
// guest/host, or the admin moderating the case.
func (s *Service) canParticipate(ctx context.Context, d *dispute.Dispute, actorID uuid.UUID) bool {
	if d.OpenerID == actorID || d.AdminID == actorID {
		return true
	}
	b, err := s.bookings.FindByID(ctx, d.BookingID)
	if err != nil {
		return false
	}
	if b.GuestID == actorID {
		return true
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return false
	}
	return prop.HostID == actorID
}
