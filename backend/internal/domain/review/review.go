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
	Rating     int // 1..5 — the overall/headline rating
	Comment    string
	// Categories holds optional per-aspect sub-ratings (only meaningful for
	// KindGuestToProperty); a zero in any field means "not rated".
	Categories CategoryRatings
	// Response is the host's public reply to a guest's property review (only
	// meaningful for KindGuestToProperty); empty when there is none.
	Response    string
	RespondedAt *time.Time
	CreatedAt   time.Time
	// UpdatedAt is set when the author edits the review; nil while untouched.
	UpdatedAt *time.Time
}

// EditWindow is how long after posting an author may still edit or delete their
// own review. After this, the review is locked to keep public ratings stable.
const EditWindow = 48 * time.Hour

// EditableAt reports whether the review is still within its edit/delete window
// at the given time.
func (r *Review) EditableAt(now time.Time) bool {
	return !now.After(r.CreatedAt.Add(EditWindow))
}

// Edit replaces the headline rating and comment of a review, applying the same
// validation as creation. Callers enforce authorship and the edit window.
func (r *Review) Edit(rating int, comment string) error {
	if rating < 1 || rating > 5 {
		return shared.NewValidationError("rating must be between 1 and 5")
	}
	comment = strings.TrimSpace(comment)
	if len(comment) > 2000 {
		return shared.NewValidationError("comment is too long")
	}
	now := time.Now().UTC()
	r.Rating = rating
	r.Comment = comment
	r.UpdatedAt = &now
	return nil
}

// CategoryRatings holds a guest's optional per-aspect sub-ratings for a property
// review. Each is 1..5, or 0 when the guest did not rate that aspect.
type CategoryRatings struct {
	Cleanliness   int
	Accuracy      int
	Communication int
	Location      int
	CheckIn       int
	Value         int
}

// each returns the six sub-ratings in a stable order for iteration.
func (c CategoryRatings) each() []int {
	return []int{c.Cleanliness, c.Accuracy, c.Communication, c.Location, c.CheckIn, c.Value}
}

// Any reports whether at least one aspect was rated.
func (c CategoryRatings) Any() bool {
	for _, v := range c.each() {
		if v > 0 {
			return true
		}
	}
	return false
}

func (c CategoryRatings) validate() error {
	for _, v := range c.each() {
		if v < 0 || v > 5 {
			return shared.NewValidationError("category ratings must be between 1 and 5")
		}
	}
	return nil
}

// SetCategories records the per-aspect sub-ratings on a property review.
func (r *Review) SetCategories(c CategoryRatings) error {
	if r.Kind != KindGuestToProperty {
		return shared.NewValidationError("only a guest's property review has category ratings")
	}
	if err := c.validate(); err != nil {
		return err
	}
	r.Categories = c
	return nil
}

// CategoryAverages is a read-model of the mean per-aspect rating across a
// property's reviews (0 when no review rated that aspect).
type CategoryAverages struct {
	Cleanliness   float64
	Accuracy      float64
	Communication float64
	Location      float64
	CheckIn       float64
	Value         float64
}

// Any reports whether at least one aspect has an average.
func (c CategoryAverages) Any() bool {
	return c.Cleanliness > 0 || c.Accuracy > 0 || c.Communication > 0 ||
		c.Location > 0 || c.CheckIn > 0 || c.Value > 0
}

// AverageCategories computes the mean of each aspect across the given ratings,
// ignoring zeros (aspects the guest left unrated). An aspect with no ratings at
// all comes back as 0. Shared by the repositories so the in-memory and SQL
// read-models agree.
func AverageCategories(ratings []CategoryRatings) CategoryAverages {
	var sums, counts [6]int
	for _, r := range ratings {
		for i, v := range r.each() {
			if v > 0 {
				sums[i] += v
				counts[i]++
			}
		}
	}
	avg := func(i int) float64 {
		if counts[i] == 0 {
			return 0
		}
		return float64(sums[i]) / float64(counts[i])
	}
	return CategoryAverages{
		Cleanliness:   avg(0),
		Accuracy:      avg(1),
		Communication: avg(2),
		Location:      avg(3),
		CheckIn:       avg(4),
		Value:         avg(5),
	}
}

// SetResponse records (or replaces) the host's public reply to a property
// review. Only guest-to-property reviews can receive a host response.
func (r *Review) SetResponse(text string) error {
	if r.Kind != KindGuestToProperty {
		return shared.NewValidationError("only a guest's property review can receive a host response")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return shared.NewValidationError("a response is required")
	}
	if len(text) > 2000 {
		return shared.NewValidationError("response is too long")
	}
	now := time.Now().UTC()
	r.Response = text
	r.RespondedAt = &now
	return nil
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
// (a property or a guest, depending on the query). Categories is populated only
// for property summaries.
type Summary struct {
	SubjectID     uuid.UUID
	AverageRating float64
	Count         int64
	Categories    CategoryAverages
}
