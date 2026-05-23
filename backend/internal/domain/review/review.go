// Package review is the bounded context for guest reviews of stays.
package review

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Review is the aggregate root for a guest's review of a completed stay.
type Review struct {
	ID         uuid.UUID
	PropertyID uuid.UUID
	BookingID  uuid.UUID
	GuestID    uuid.UUID
	Rating     int // 1..5
	Comment    string
	CreatedAt  time.Time
}

// NewReview creates a Review, enforcing the rating range invariant.
func NewReview(propertyID, bookingID, guestID uuid.UUID, rating int, comment string) (*Review, error) {
	if rating < 1 || rating > 5 {
		return nil, shared.NewValidationError("rating must be between 1 and 5")
	}
	comment = strings.TrimSpace(comment)
	if len(comment) > 2000 {
		return nil, shared.NewValidationError("comment is too long")
	}
	return &Review{
		ID:         uuid.New(),
		PropertyID: propertyID,
		BookingID:  bookingID,
		GuestID:    guestID,
		Rating:     rating,
		Comment:    comment,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

// Summary is a read-model value object aggregating ratings for a property.
type Summary struct {
	PropertyID    uuid.UUID
	AverageRating float64
	Count         int64
}
