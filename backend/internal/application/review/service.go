// Package reviewapp contains review use cases. A guest may only review a
// completed stay, and only once per booking.
package reviewapp

import (
	"context"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates review use cases.
type Service struct {
	reviews  review.Repository
	bookings booking.Repository
}

// NewService wires the review application service.
func NewService(reviews review.Repository, bookings booking.Repository) *Service {
	return &Service{reviews: reviews, bookings: bookings}
}

// CreateInput carries data to publish a review.
type CreateInput struct {
	GuestID   uuid.UUID
	BookingID uuid.UUID
	Rating    int
	Comment   string
}

// Create publishes a review for a completed booking owned by the guest.
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

	exists, err := s.reviews.ExistsForBooking(ctx, in.BookingID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, shared.NewValidationError("this booking has already been reviewed")
	}

	r, err := review.NewReview(b.PropertyID, b.ID, in.GuestID, in.Rating, in.Comment)
	if err != nil {
		return nil, err
	}
	if err := s.reviews.Create(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// ListByProperty returns reviews for a property.
func (s *Service) ListByProperty(ctx context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*review.Review], error) {
	return s.reviews.ListByProperty(ctx, propertyID, page)
}

// Summary returns aggregate rating stats for a property.
func (s *Service) Summary(ctx context.Context, propertyID uuid.UUID) (review.Summary, error) {
	return s.reviews.SummaryForProperty(ctx, propertyID)
}
