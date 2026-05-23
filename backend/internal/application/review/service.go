// Package reviewapp contains review use cases. Reviews are bidirectional: a
// guest reviews the property after a completed stay, and the host reviews the
// guest. Each side may review at most once per booking.
package reviewapp

import (
	"context"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates review use cases.
type Service struct {
	reviews    review.Repository
	bookings   booking.Repository
	properties property.Repository
}

// NewService wires the review application service.
func NewService(reviews review.Repository, bookings booking.Repository, properties property.Repository) *Service {
	return &Service{reviews: reviews, bookings: bookings, properties: properties}
}

// CreateInput carries data to publish a property review (guest -> property).
type CreateInput struct {
	GuestID   uuid.UUID
	BookingID uuid.UUID
	Rating    int
	Comment   string
}

// Create publishes a guest's review of the property for a completed booking
// they own.
func (s *Service) Create(ctx context.Context, in CreateInput) (*review.Review, error) {
	b, err := s.bookings.FindByID(ctx, in.BookingID)
	if err != nil {
		return nil, err
	}
	if b.GuestID != in.GuestID {
		return nil, shared.ErrForbidden
	}
	if b.Status != booking.StatusCompleted {
		return nil, shared.NewValidationError("only completed stays can be reviewed")
	}

	if err := s.ensureNotReviewed(ctx, in.BookingID, review.KindGuestToProperty); err != nil {
		return nil, err
	}

	r, err := review.NewPropertyReview(b.ID, b.PropertyID, in.GuestID, in.Rating, in.Comment)
	if err != nil {
		return nil, err
	}
	if err := s.reviews.Create(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// GuestReviewInput carries data for a host's review of the guest.
type GuestReviewInput struct {
	HostID    uuid.UUID
	BookingID uuid.UUID
	Rating    int
	Comment   string
}

// CreateGuestReview publishes the host's review of the guest for a completed
// booking on a property the host owns.
func (s *Service) CreateGuestReview(ctx context.Context, in GuestReviewInput) (*review.Review, error) {
	b, err := s.bookings.FindByID(ctx, in.BookingID)
	if err != nil {
		return nil, err
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(in.HostID) {
		return nil, shared.ErrForbidden
	}
	if b.Status != booking.StatusCompleted {
		return nil, shared.NewValidationError("only completed stays can be reviewed")
	}

	if err := s.ensureNotReviewed(ctx, in.BookingID, review.KindHostToGuest); err != nil {
		return nil, err
	}

	r, err := review.NewGuestReview(b.ID, b.PropertyID, in.HostID, b.GuestID, in.Rating, in.Comment)
	if err != nil {
		return nil, err
	}
	if err := s.reviews.Create(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) ensureNotReviewed(ctx context.Context, bookingID uuid.UUID, kind review.Kind) error {
	exists, err := s.reviews.ExistsForBookingKind(ctx, bookingID, kind)
	if err != nil {
		return err
	}
	if exists {
		return shared.NewValidationError("this booking has already been reviewed")
	}
	return nil
}

// ListByProperty returns property reviews for a property.
func (s *Service) ListByProperty(ctx context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*review.Review], error) {
	return s.reviews.ListByProperty(ctx, propertyID, page)
}

// Summary returns aggregate property-rating stats.
func (s *Service) Summary(ctx context.Context, propertyID uuid.UUID) (review.Summary, error) {
	return s.reviews.SummaryForProperty(ctx, propertyID)
}

// ListAboutGuest returns the reviews hosts have written about a guest.
func (s *Service) ListAboutGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*review.Review], error) {
	return s.reviews.ListAboutGuest(ctx, guestID, page)
}

// SummaryForGuest returns aggregate rating stats about a guest.
func (s *Service) SummaryForGuest(ctx context.Context, guestID uuid.UUID) (review.Summary, error) {
	return s.reviews.SummaryForGuest(ctx, guestID)
}
