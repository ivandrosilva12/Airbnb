// Package reviewapp contains review use cases. Reviews are bidirectional: a
// guest reviews the property after a completed stay, and the host reviews the
// guest. Each side may review at most once per booking.
package reviewapp

import (
	"context"
	"sort"
	"time"

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

// RespondToReview lets the property's host publish a public reply to a guest's
// review. Only the owning host may respond, and only to a property review.
func (s *Service) RespondToReview(ctx context.Context, hostID, reviewID uuid.UUID, text string) (*review.Review, error) {
	rv, err := s.reviews.FindByID(ctx, reviewID)
	if err != nil {
		return nil, err
	}
	prop, err := s.properties.FindByID(ctx, rv.PropertyID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(hostID) {
		return nil, shared.ErrForbidden
	}
	if err := rv.SetResponse(text); err != nil {
		return nil, err
	}
	if err := s.reviews.Update(ctx, rv); err != nil {
		return nil, err
	}
	return rv, nil
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
	s.refreshPropertyRating(ctx, b.PropertyID)
	return r, nil
}

// refreshPropertyRating recomputes and persists the listing's cached rating,
// then re-evaluates the owning host's Superhost status. Best-effort: a failure
// here must not fail the review that was just created.
func (s *Service) refreshPropertyRating(ctx context.Context, propertyID uuid.UUID) {
	summary, err := s.reviews.SummaryForProperty(ctx, propertyID)
	if err != nil {
		return
	}
	_ = s.properties.UpdateRating(ctx, propertyID, summary.AverageRating, int(summary.Count))
	s.refreshHostSuperhost(ctx, propertyID)
}

// refreshHostSuperhost recomputes whether the listing's host qualifies for the
// Superhost badge (from their review-weighted rating across all listings) and
// fans the result out across the host's listings. Best-effort.
func (s *Service) refreshHostSuperhost(ctx context.Context, propertyID uuid.UUID) {
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return
	}
	avg, count, err := s.properties.HostRatingAggregate(ctx, prop.HostID)
	if err != nil {
		return
	}
	_ = s.properties.SetHostSuperhost(ctx, prop.HostID, property.QualifiesAsSuperhost(avg, count))
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

// PendingReview is a completed stay the guest has not reviewed yet — the
// backbone of the post-stay review prompt.
type PendingReview struct {
	BookingID     uuid.UUID
	PropertyID    uuid.UUID
	PropertyTitle string
	CheckIn       time.Time
	CheckOut      time.Time
}

// PendingForGuest returns the guest's completed stays that still await their
// property review, most recent check-out first.
func (s *Service) PendingForGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) ([]PendingReview, error) {
	res, err := s.bookings.ListByGuest(ctx, guestID, page)
	if err != nil {
		return nil, err
	}
	pending := make([]PendingReview, 0)
	for _, b := range res.Items {
		if b.Status != booking.StatusCompleted {
			continue
		}
		reviewed, err := s.reviews.ExistsForBookingKind(ctx, b.ID, review.KindGuestToProperty)
		if err != nil {
			return nil, err
		}
		if reviewed {
			continue
		}
		title := ""
		if prop, err := s.properties.FindByID(ctx, b.PropertyID); err == nil {
			title = prop.Title
		}
		pending = append(pending, PendingReview{
			BookingID:     b.ID,
			PropertyID:    b.PropertyID,
			PropertyTitle: title,
			CheckIn:       b.Dates.CheckIn,
			CheckOut:      b.Dates.CheckOut,
		})
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CheckOut.After(pending[j].CheckOut)
	})
	return pending, nil
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
