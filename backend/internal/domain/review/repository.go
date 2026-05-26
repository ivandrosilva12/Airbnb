package review

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the Review aggregate.
type Repository interface {
	Create(ctx context.Context, r *Review) error
	// FindByID loads a single review.
	FindByID(ctx context.Context, id uuid.UUID) (*Review, error)
	// Update persists mutable fields (the host response, and author edits to the
	// rating/comment/categories).
	Update(ctx context.Context, r *Review) error
	// Delete removes a review (author deletion within the edit window).
	Delete(ctx context.Context, id uuid.UUID) error
	// ExistsForBookingKind reports whether a review of the given kind already
	// exists for the booking (at most one per direction).
	ExistsForBookingKind(ctx context.Context, bookingID uuid.UUID, kind Kind) (bool, error)

	// Guest-facing property reviews.
	ListByProperty(ctx context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*Review], error)
	SummaryForProperty(ctx context.Context, propertyID uuid.UUID) (Summary, error)

	// Host-written reviews about a guest.
	ListAboutGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*Review], error)
	SummaryForGuest(ctx context.Context, guestID uuid.UUID) (Summary, error)

	// AnonymizeByAuthor scrubs the free-text comment of reviews written by an
	// author (GDPR erasure), keeping the rating and the review's existence so
	// listing/guest aggregates stay intact.
	AnonymizeByAuthor(ctx context.Context, authorID uuid.UUID) error
}
