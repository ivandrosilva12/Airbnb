// Package reviewapp contains review use cases. Reviews are bidirectional: a
// guest reviews the property after a completed stay, and the host reviews the
// guest. Each side may review at most once per booking.
package reviewapp

import (
	"context"
	"sort"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// ReviewMetrics is the optional Prometheus sink for ReviewSubmitted
// domain events. It is defined here (rather than imported from the
// observability package) so the application layer does not depend on
// infrastructure: the wiring in main.go passes the concrete
// *observability.Metrics, which satisfies this interface via an inline
// adapter. direction is the review's Kind — one of "guest_to_property"
// or "host_to_guest" (S151 — follow-on to S136/S147/S148).
type ReviewMetrics interface {
	IncReviewSubmitted(direction string)
}

// Service orchestrates review use cases.
type Service struct {
	reviews    review.Repository
	bookings   booking.Repository
	properties property.Repository
	// uow, when wired (S136), routes the synchronous Create writes
	// (guest-to-property and host-to-guest) through a UnitOfWork so the
	// reviews row and the ReviewSubmitted outbox event commit atomically —
	// closing the "row landed but event was lost" gap that existed when
	// the service wrote through a pool-bound repo with no outbox event at
	// all. Nil falls back to the legacy pool-bound path so older tests
	// that don't wire a UoW keep working.
	uow port.UnitOfWork
	// metrics, when wired via WithMetrics, counts each ReviewSubmitted
	// event published from persistCreate by direction so ops can graph
	// the guest-to-property / host-to-guest review flow. Optional — nil
	// keeps observability off, matching the pre-S151 behaviour for
	// focused unit tests (S151).
	metrics ReviewMetrics
}

// NewService wires the review application service.
func NewService(reviews review.Repository, bookings booking.Repository, properties property.Repository) *Service {
	return &Service{reviews: reviews, bookings: bookings, properties: properties}
}

// WithUnitOfWork wires a UoW into the service so synchronous review-create
// writes (Create / CreateGuestReview) commit the reviews INSERT atomically
// with the ReviewSubmitted outbox event (S136 — Review UoW slice).
// Without it, the service falls back to the legacy pool-bound write path
// (no outbox event emitted). Returns the same service for chained
// construction.
func (s *Service) WithUnitOfWork(uow port.UnitOfWork) *Service {
	s.uow = uow
	return s
}

// WithMetrics plugs in the Prometheus sink so every ReviewSubmitted
// event published from persistCreate increments the
// ReviewsSubmittedTotal counter labeled by direction (S151). Nil leaves
// observability off. Returns the receiver for fluent wiring — main.go
// calls .WithMetrics(metrics) right after .WithUnitOfWork, mirroring
// the offer/dispute services.
func (s *Service) WithMetrics(m ReviewMetrics) *Service {
	s.metrics = m
	return s
}

// persistCreate writes a new review row + a ReviewSubmitted outbox event
// atomically. When a UoW is wired (S136), both writes land in the same
// transaction — so a crash between them no longer drops the event. When
// the UoW is absent (legacy tests / call sites built before S136), it
// falls back to the pool-bound repo and skips the event (matching the
// pre-S136 behaviour). The tx-bound Reviews repo may be nil when the
// UoW factory wasn't extended (e.g. the in-memory UoW used by an older
// test that doesn't call WithReviews) — in that case we still route the
// write through the UoW (so the outbox append participates in the same
// commit) but using the service's pool-bound repo.
func (s *Service) persistCreate(ctx context.Context, r *review.Review) error {
	if s.uow == nil {
		return s.reviews.Create(ctx, r)
	}
	if err := s.uow.Run(ctx, func(tx port.Tx) error {
		repo := s.reviews
		if tx.Reviews != nil {
			repo = tx.Reviews
		}
		if err := repo.Create(ctx, r); err != nil {
			return err
		}
		direction := string(r.Kind)
		rec, err := event.NewRecord(event.ReviewSubmitted{
			ReviewID:   r.ID,
			BookingID:  r.BookingID,
			PropertyID: r.PropertyID,
			AuthorID:   r.AuthorID,
			GuestID:    r.GuestID,
			Direction:  direction,
			Rating:     r.Rating,
		})
		if err != nil {
			return err
		}
		return tx.Outbox.Append(ctx, rec)
	}); err != nil {
		return err
	}
	// Only bump the counter after the UoW commits successfully so a
	// commit-time rollback never inflates the metric (S151).
	if s.metrics != nil {
		s.metrics.IncReviewSubmitted(string(r.Kind))
	}
	return nil
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

// EditInput carries an author's change to their own review.
type EditInput struct {
	AuthorID   uuid.UUID
	ReviewID   uuid.UUID
	Rating     int
	Comment    string
	Categories review.CategoryRatings
}

// EditOwnReview lets the author change the rating/comment (and, for a property
// review, the per-aspect category ratings) of their own review, but only within
// the edit window. A changed rating re-derives the listing's cached rating.
func (s *Service) EditOwnReview(ctx context.Context, in EditInput) (*review.Review, error) {
	rv, err := s.reviews.FindByID(ctx, in.ReviewID)
	if err != nil {
		return nil, err
	}
	if rv.AuthorID != in.AuthorID {
		return nil, shared.ErrForbidden
	}
	if !rv.EditableAt(time.Now().UTC()) {
		return nil, shared.NewValidationError("the edit window for this review has passed")
	}
	if err := rv.Edit(in.Rating, in.Comment); err != nil {
		return nil, err
	}
	if rv.Kind == review.KindGuestToProperty {
		if err := rv.SetCategories(in.Categories); err != nil {
			return nil, err
		}
	}
	if err := s.reviews.Update(ctx, rv); err != nil {
		return nil, err
	}
	if rv.Kind == review.KindGuestToProperty {
		s.refreshPropertyRating(ctx, rv.PropertyID)
	}
	return rv, nil
}

// DeleteOwnReview lets the author remove their own review within the edit
// window. Deleting a property review re-derives the listing's cached rating.
func (s *Service) DeleteOwnReview(ctx context.Context, authorID, reviewID uuid.UUID) error {
	rv, err := s.reviews.FindByID(ctx, reviewID)
	if err != nil {
		return err
	}
	if rv.AuthorID != authorID {
		return shared.ErrForbidden
	}
	if !rv.EditableAt(time.Now().UTC()) {
		return shared.NewValidationError("the deletion window for this review has passed")
	}
	if err := s.reviews.Delete(ctx, reviewID); err != nil {
		return err
	}
	if rv.Kind == review.KindGuestToProperty {
		s.refreshPropertyRating(ctx, rv.PropertyID)
	}
	return nil
}

// CreateInput carries data to publish a property review (guest -> property).
type CreateInput struct {
	GuestID    uuid.UUID
	BookingID  uuid.UUID
	Rating     int
	Comment    string
	Categories review.CategoryRatings
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
	if err := r.SetCategories(in.Categories); err != nil {
		return nil, err
	}
	if err := s.persistCreate(ctx, r); err != nil {
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
	if err := s.persistCreate(ctx, r); err != nil {
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
