// Package experience owns the Experience aggregate — a host-led activity
// (cooking class, food tour, surf lesson, museum walk) sold per-guest
// rather than per-night. Separate aggregate from property: an experience
// has no overnight stay, no cleaning fee, no per-stay availability
// pattern; it has a fixed duration, a per-guest price, and a category.
//
// The first slice is intentionally a stub: no bookings, no availability
// slots, no co-host roles. The aggregate + CRUD + public search are
// enough to expose the catalogue surface and let later slices layer on
// the "book a session at time T" semantics without re-shaping the
// shipping payload.
package experience

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Category is the closed enum the search UI groups by. Renaming or
// removing a value is a breaking change for the catalogue page and any
// saved-search keyed on a category.
type Category string

const (
	CategoryCooking  Category = "cooking"
	CategoryTour     Category = "tour"
	CategorySport    Category = "sport"
	CategoryArt      Category = "art"
	CategoryWellness Category = "wellness"
	CategoryOther    Category = "other"
)

// Valid reports whether c is one of the recognised categories.
func (c Category) Valid() bool {
	switch c {
	case CategoryCooking, CategoryTour, CategorySport, CategoryArt, CategoryWellness, CategoryOther:
		return true
	}
	return false
}

// Status is the publication state of an experience listing. Same shape
// as property.Status to keep the host/admin UI familiar.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusSuspended Status = "suspended"
)

// Valid reports whether s is one of the recognised states.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusPublished, StatusSuspended:
		return true
	}
	return false
}

// Address is intentionally a minimal copy of property.Address (rather
// than importing it) so the experience BC does not depend on the
// property BC. Coordinates are required so the catalogue can render
// the activity on a map.
type Address struct {
	City      string
	Country   string
	Latitude  float64
	Longitude float64
}

func (a Address) validate() error {
	if strings.TrimSpace(a.City) == "" || strings.TrimSpace(a.Country) == "" {
		return shared.NewValidationError("experience.address: city and country are required")
	}
	if a.Latitude < -90 || a.Latitude > 90 || a.Longitude < -180 || a.Longitude > 180 {
		return shared.NewValidationError("experience.address: coordinates out of range")
	}
	return nil
}

// Photo mirrors property.Photo — the shape is small enough to inline
// rather than couple to the property BC.
type Photo struct {
	ID        uuid.UUID
	ObjectKey string
	URL       string
	Position  int
}

// Tunables — bounded ranges enforced at the domain edge. Keeping them
// as constants makes the rule discoverable in code-review without
// hunting for inline literals.
const (
	MinDurationMinutes = 30
	MaxDurationMinutes = 24 * 60 // a single day — multi-day retreats are a different product
	MaxTitleLength     = 200
	MinMaxGuests       = 1
	MaxMaxGuests       = 50 // a workshop-sized cap; bigger group activities belong elsewhere
)

// Experience is the aggregate root. Pricing is per-guest, never per-
// night — the BC owns no overnight semantics.
type Experience struct {
	ID              uuid.UUID
	HostID          uuid.UUID
	Title           string
	Description     string
	Category        Category
	Status          Status
	Address         Address
	DurationMinutes int
	MaxGuests       int
	PricePerGuest   shared.Money
	// Language is an ISO 639-1 code identifying the language the
	// activity is delivered in (e.g. "en", "pt"). Required because
	// language is the #1 filter guests use when picking an activity
	// abroad.
	Language  string
	Photos    []Photo
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewExperience creates a draft Experience aggregate, validating every
// invariant the rest of the system can rely on.
func NewExperience(
	hostID uuid.UUID,
	title, description string,
	category Category,
	address Address,
	durationMinutes, maxGuests int,
	pricePerGuest shared.Money,
	language string,
) (*Experience, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, shared.NewValidationError("experience: title is required")
	}
	if len(title) > MaxTitleLength {
		return nil, shared.NewValidationError("experience: title too long")
	}
	if hostID == uuid.Nil {
		return nil, shared.NewValidationError("experience: hostID is required")
	}
	if !category.Valid() {
		return nil, shared.NewValidationError("experience: unknown category " + string(category))
	}
	if err := address.validate(); err != nil {
		return nil, err
	}
	if durationMinutes < MinDurationMinutes || durationMinutes > MaxDurationMinutes {
		return nil, shared.NewValidationError("experience: duration out of allowed range")
	}
	if maxGuests < MinMaxGuests || maxGuests > MaxMaxGuests {
		return nil, shared.NewValidationError("experience: maxGuests out of allowed range")
	}
	if pricePerGuest.AmountCents() < 0 {
		return nil, shared.NewValidationError("experience: pricePerGuest cannot be negative")
	}
	lang := strings.ToLower(strings.TrimSpace(language))
	if len(lang) != 2 {
		return nil, shared.NewValidationError("experience: language must be a 2-letter ISO code")
	}
	now := time.Now().UTC()
	return &Experience{
		ID:              uuid.New(),
		HostID:          hostID,
		Title:           title,
		Description:    strings.TrimSpace(description),
		Category:        category,
		Status:          StatusDraft,
		Address:         address,
		DurationMinutes: durationMinutes,
		MaxGuests:       maxGuests,
		PricePerGuest:   pricePerGuest,
		Language:        lang,
		Photos:          nil,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

// Publish moves the experience from draft to published, refusing the
// transition if the listing is missing the bare-minimum content a guest
// expects to see in the catalogue (title is already enforced at New;
// description and at least one photo are the publish gate).
//
// Suspended → Published is allowed (admin re-enabling after a takedown).
// Already-published is a no-op.
func (e *Experience) Publish() error {
	if e.Status == StatusPublished {
		return nil
	}
	if strings.TrimSpace(e.Description) == "" {
		return shared.NewValidationError("experience: description is required before publishing")
	}
	if len(e.Photos) == 0 {
		return shared.NewValidationError("experience: at least one photo is required before publishing")
	}
	e.Status = StatusPublished
	e.touch()
	return nil
}

// Suspend takes a listing off the catalogue (admin moderation or host
// "pause"). Idempotent.
func (e *Experience) Suspend() {
	if e.Status == StatusSuspended {
		return
	}
	e.Status = StatusSuspended
	e.touch()
}

// UpdateBasics edits the host-editable fields in one shot. Re-runs the
// same validators NewExperience does so a PATCH cannot land an invalid
// state through the back door.
func (e *Experience) UpdateBasics(
	title, description string,
	category Category,
	address Address,
	durationMinutes, maxGuests int,
	pricePerGuest shared.Money,
	language string,
) error {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > MaxTitleLength {
		return shared.NewValidationError("experience: title is required (max 200 chars)")
	}
	if !category.Valid() {
		return shared.NewValidationError("experience: unknown category " + string(category))
	}
	if err := address.validate(); err != nil {
		return err
	}
	if durationMinutes < MinDurationMinutes || durationMinutes > MaxDurationMinutes {
		return shared.NewValidationError("experience: duration out of allowed range")
	}
	if maxGuests < MinMaxGuests || maxGuests > MaxMaxGuests {
		return shared.NewValidationError("experience: maxGuests out of allowed range")
	}
	if pricePerGuest.AmountCents() < 0 {
		return shared.NewValidationError("experience: pricePerGuest cannot be negative")
	}
	lang := strings.ToLower(strings.TrimSpace(language))
	if len(lang) != 2 {
		return shared.NewValidationError("experience: language must be a 2-letter ISO code")
	}
	e.Title = title
	e.Description = strings.TrimSpace(description)
	e.Category = category
	e.Address = address
	e.DurationMinutes = durationMinutes
	e.MaxGuests = maxGuests
	e.PricePerGuest = pricePerGuest
	e.Language = lang
	e.touch()
	return nil
}

// AddPhoto appends a photo. The host UI is expected to upload then call
// this — there's no upper limit at the domain edge (storage and the
// transport layer impose practical caps).
func (e *Experience) AddPhoto(objectKey, url string) {
	e.Photos = append(e.Photos, Photo{
		ID:        uuid.New(),
		ObjectKey: objectKey,
		URL:       url,
		Position:  len(e.Photos),
	})
	e.touch()
}

func (e *Experience) touch() {
	e.UpdatedAt = time.Now().UTC()
}
