// Package property is the bounded context for accommodation listings.
package property

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// PropertyType enumerates the kinds of accommodation.
type PropertyType string

const (
	TypeApartment PropertyType = "apartment"
	TypeHouse     PropertyType = "house"
	TypeRoom      PropertyType = "room"
	TypeVilla     PropertyType = "villa"
	TypeCabin     PropertyType = "cabin"
)

func (t PropertyType) Valid() bool {
	switch t {
	case TypeApartment, TypeHouse, TypeRoom, TypeVilla, TypeCabin:
		return true
	default:
		return false
	}
}

// Status enumerates the publication lifecycle of a listing.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusSuspended Status = "suspended"
)

// Address is a value object describing where a property is located.
type Address struct {
	Line1      string
	City       string
	Country    string
	PostalCode string
	Latitude   float64
	Longitude  float64
}

func (a Address) validate() error {
	if strings.TrimSpace(a.City) == "" || strings.TrimSpace(a.Country) == "" {
		return shared.NewValidationError("address city and country are required")
	}
	if a.Latitude < -90 || a.Latitude > 90 || a.Longitude < -180 || a.Longitude > 180 {
		return shared.NewValidationError("address coordinates are out of range")
	}
	return nil
}

// Photo is a value object pointing to a stored media object.
type Photo struct {
	ID        uuid.UUID
	ObjectKey string // key in the object store (MinIO)
	URL       string // public URL
	Position  int
}

// Property is the aggregate root for a listing.
type Property struct {
	ID            uuid.UUID
	HostID        uuid.UUID
	Title         string
	Description   string
	Type          PropertyType
	Status        Status
	Address       Address
	PricePerNight shared.Money
	CleaningFee   shared.Money
	MaxGuests     int
	Bedrooms      int
	Beds          int
	Bathrooms     int
	Amenities     []string
	Photos        []Photo
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewProperty creates a draft Property aggregate enforcing invariants.
func NewProperty(
	hostID uuid.UUID,
	title, description string,
	pType PropertyType,
	address Address,
	price, cleaningFee shared.Money,
	maxGuests, bedrooms, beds, bathrooms int,
	amenities []string,
) (*Property, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, shared.NewValidationError("title is required")
	}
	if !pType.Valid() {
		return nil, shared.NewValidationError("invalid property type")
	}
	if err := address.validate(); err != nil {
		return nil, err
	}
	if cleaningFee.Currency() != price.Currency() {
		return nil, shared.NewValidationError("cleaning fee must use the same currency as the nightly price")
	}
	if maxGuests < 1 {
		return nil, shared.NewValidationError("maxGuests must be at least 1")
	}
	if bedrooms < 0 || beds < 0 || bathrooms < 0 {
		return nil, shared.NewValidationError("room counts cannot be negative")
	}

	now := time.Now().UTC()
	return &Property{
		ID:            uuid.New(),
		HostID:        hostID,
		Title:         title,
		Description:   strings.TrimSpace(description),
		Type:          pType,
		Status:        StatusDraft,
		Address:       address,
		PricePerNight: price,
		CleaningFee:   cleaningFee,
		MaxGuests:     maxGuests,
		Bedrooms:      bedrooms,
		Beds:          beds,
		Bathrooms:     bathrooms,
		Amenities:     dedupe(amenities),
		Photos:        []Photo{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

// Publish moves a listing into the published state so guests can book it.
func (p *Property) Publish() error {
	if len(p.Photos) == 0 {
		return shared.NewValidationError("a property needs at least one photo before publishing")
	}
	p.Status = StatusPublished
	p.touch()
	return nil
}

// Suspend hides a listing from search without deleting it.
func (p *Property) Suspend() {
	p.Status = StatusSuspended
	p.touch()
}

// Unsuspend restores a suspended listing back to published.
func (p *Property) Unsuspend() error {
	if p.Status != StatusSuspended {
		return shared.NewValidationError("only suspended listings can be reinstated")
	}
	p.Status = StatusPublished
	p.touch()
	return nil
}

// AddPhoto appends a photo to the listing.
func (p *Property) AddPhoto(objectKey, url string) Photo {
	photo := Photo{
		ID:        uuid.New(),
		ObjectKey: objectKey,
		URL:       url,
		Position:  len(p.Photos),
	}
	p.Photos = append(p.Photos, photo)
	p.touch()
	return photo
}

// UpdateDetails mutates editable descriptive fields.
func (p *Property) UpdateDetails(title, description string, price, cleaningFee shared.Money, maxGuests int) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return shared.NewValidationError("title is required")
	}
	if cleaningFee.Currency() != price.Currency() {
		return shared.NewValidationError("cleaning fee must use the same currency as the nightly price")
	}
	if maxGuests < 1 {
		return shared.NewValidationError("maxGuests must be at least 1")
	}
	p.Title = title
	p.Description = strings.TrimSpace(description)
	p.PricePerNight = price
	p.CleaningFee = cleaningFee
	p.MaxGuests = maxGuests
	p.touch()
	return nil
}

// CanBeBookedBy enforces the rule that hosts cannot book their own property
// and that the listing is published.
func (p *Property) CanBeBookedBy(guestID uuid.UUID) error {
	if p.Status != StatusPublished {
		return shared.NewValidationError("property is not available for booking")
	}
	if p.HostID == guestID {
		return shared.NewValidationError("a host cannot book their own property")
	}
	return nil
}

// IsOwnedBy reports whether the given user is the host.
func (p *Property) IsOwnedBy(userID uuid.UUID) bool { return p.HostID == userID }

func (p *Property) touch() { p.UpdatedAt = time.Now().UTC() }

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
