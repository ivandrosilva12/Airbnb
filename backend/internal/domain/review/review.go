// Package review is the bounded context for reviews of a completed stay. Reviews
// are bidirectional: a guest reviews the property, and a host reviews the guest.
package review

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Kind is the direction of a review.
type Kind string

const (
	// KindGuestToProperty is a guest's review of the property/stay.
	KindGuestToProperty Kind = "guest_to_property"
	// KindHostToGuest is a host's review of the guest.
	KindHostToGuest Kind = "host_to_guest"
)

// Review is the aggregate root for a review of a completed booking.
type Review struct {
	ID         uuid.UUID
	BookingID  uuid.UUID
	PropertyID uuid.UUID
	AuthorID   uuid.UUID // who wrote the review
	GuestID    uuid.UUID // the booking's guest (the subject when Kind is host_to_guest)
	Kind       Kind
	Rating     int // 1..5
	Comment    string
	CreatedAt  time.Time
}

func newReview(kind Kind, bookingID, propertyID, authorID, guestID uuid.UUID, rating int, comment string) (*Review, error) {
	if rating < 1 || rating > 5 {
		return nil, shared.NewValidationError("rating must be between 1 and 5")
	}
	comment = strings.TrimSpace(comment)
	if len(comment) > 2000 {
		return nil, shared.NewValidationError("comment is too long")
	}
	return &Review{
		ID:         uuid.New(),
		BookingID:  bookingID,
		PropertyID: propertyID,
		AuthorID:   authorID,
		GuestID:    guestID,
		Kind:       kind,
		Rating:     rating,
		Comment:    comment,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

// NewPropertyReview creates a guest's review of the property they stayed in.
func NewPropertyReview(bookingID, propertyID, guestID uuid.UUID, rating int, comment string) (*Review, error) {
	return newReview(KindGuestToProperty, bookingID, propertyID, guestID, guestID, rating, comment)
}

// NewGuestReview creates a host's review of the guest for a completed booking.
func NewGuestReview(bookingID, propertyID, hostID, guestID uuid.UUID, rating int, comment string) (*Review, error) {
	return newReview(KindHostToGuest, bookingID, propertyID, hostID, guestID, rating, comment)
}

// Summary is a read-model value object aggregating ratings about a subject
// (a property or a guest, depending on the query).
type Summary struct {
	SubjectID     uuid.UUID
	AverageRating float64
	Count         int64
}
