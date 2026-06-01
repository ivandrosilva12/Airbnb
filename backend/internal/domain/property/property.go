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

// CancellationPolicy governs how much of a captured payment is refunded when a
// guest cancels, based on how many days before check-in they cancel.
type CancellationPolicy string

const (
	PolicyFlexible CancellationPolicy = "flexible"
	PolicyModerate CancellationPolicy = "moderate"
	PolicyStrict   CancellationPolicy = "strict"
)

// ValidCancellationPolicy normalises an input policy, defaulting to flexible.
func ValidCancellationPolicy(p CancellationPolicy) CancellationPolicy {
	switch p {
	case PolicyFlexible, PolicyModerate, PolicyStrict:
		return p
	default:
		return PolicyFlexible
	}
}

// RefundFraction returns the fraction (0..1) of a captured payment refunded to a
// guest who cancels daysUntilCheckIn days before arrival.
func (p CancellationPolicy) RefundFraction(daysUntilCheckIn int) float64 {
	switch p {
	case PolicyModerate:
		switch {
		case daysUntilCheckIn >= 5:
			return 1.0
		case daysUntilCheckIn >= 1:
			return 0.5
		default:
			return 0.0
		}
	case PolicyStrict:
		switch {
		case daysUntilCheckIn >= 7:
			return 1.0
		case daysUntilCheckIn >= 2:
			return 0.5
		default:
			return 0.0
		}
	default: // flexible
		if daysUntilCheckIn >= 1 {
			return 1.0
		}
		return 0.0
	}
}

// PricingPolicy holds the optional discounts and taxes a host configures for a
// listing. All values are fractions in [0,1]. Defaults are zero (no discount or
// tax). Length-of-stay discounts reduce the nightly subtotal; the tax applies to
// the discounted accommodation base plus the cleaning fee.
type PricingPolicy struct {
	WeeklyDiscountPct  float64 // applied to the subtotal for stays of >= 7 nights
	MonthlyDiscountPct float64 // applied to the subtotal for stays of >= 28 nights
	TaxRatePct         float64 // occupancy tax on the (discounted) accommodation base
	// WeekendPriceCents replaces the base nightly price on Friday and Saturday
	// nights when set (> 0). Listing-specific date rules (the pricerule context)
	// take precedence on days they cover.
	WeekendPriceCents int64
}

func clampFraction(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// Normalised returns the policy with fractions clamped to [0,1] and a negative
// weekend price treated as 0 (off).
func (p PricingPolicy) Normalised() PricingPolicy {
	weekend := p.WeekendPriceCents
	if weekend < 0 {
		weekend = 0
	}
	return PricingPolicy{
		WeeklyDiscountPct:  clampFraction(p.WeeklyDiscountPct),
		MonthlyDiscountPct: clampFraction(p.MonthlyDiscountPct),
		TaxRatePct:         clampFraction(p.TaxRatePct),
		WeekendPriceCents:  weekend,
	}
}

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
	ID                 uuid.UUID
	HostID             uuid.UUID
	Title              string
	Description        string
	Type               PropertyType
	Status             Status
	Address            Address
	PricePerNight      shared.Money
	CleaningFee        shared.Money
	MaxGuests          int
	Bedrooms           int
	Beds               int
	Bathrooms          int
	Amenities          []string
	Photos             []Photo
	CancellationPolicy CancellationPolicy
	PricingPolicy      PricingPolicy
	// InstantBook, when true, auto-confirms a guest's reservation instead of
	// holding it for the host's approval.
	InstantBook bool
	// Stay rules and per-guest pricing.
	MinNights       int          // minimum nights per reservation (>= 1)
	MaxNights       int          // maximum nights (0 = no cap)
	GuestsIncluded  int          // guests included in the base price; extras are charged
	ExtraGuestFee   shared.Money // per extra guest, per night
	SecurityDeposit shared.Money // refundable hold collected with the booking
	// AverageRating and ReviewCount are a denormalised read-model of the
	// property's guest reviews, refreshed when a review is published.
	AverageRating float64
	ReviewCount   int
	// HostIsSuperhost is a denormalised copy of the owning host's Superhost
	// status, fanned out across the host's listings when their aggregate rating
	// crosses the threshold (see QualifiesAsSuperhost). It lets search and listing
	// reads surface the badge without an extra per-host lookup.
	HostIsSuperhost bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Superhost qualification thresholds: a host earns the badge with a strong
// aggregate guest rating across enough reviews on their listings.
const (
	SuperhostMinRating  = 4.8
	SuperhostMinReviews = 10
)

// QualifiesAsSuperhost reports whether a host's aggregate guest rating (averaged
// across all their listings) and review volume earn the Superhost badge.
func QualifiesAsSuperhost(avgRating float64, reviewCount int) bool {
	return reviewCount >= SuperhostMinReviews && avgRating >= SuperhostMinRating
}

// SetRating updates the cached review aggregates for the listing.
func (p *Property) SetRating(average float64, count int) {
	p.AverageRating = average
	p.ReviewCount = count
	p.touch()
}

// SetCancellationPolicy sets the listing's cancellation policy (normalised).
func (p *Property) SetCancellationPolicy(policy CancellationPolicy) {
	p.CancellationPolicy = ValidCancellationPolicy(policy)
	p.touch()
}

// SetPricingPolicy sets the listing's discounts/tax policy (clamped to [0,1]).
func (p *Property) SetPricingPolicy(policy PricingPolicy) {
	p.PricingPolicy = policy.Normalised()
	p.touch()
}

// SetInstantBook toggles whether reservations are auto-confirmed (true) or held
// for the host's approval (false).
func (p *Property) SetInstantBook(v bool) {
	p.InstantBook = v
	p.touch()
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
		ID:                 uuid.New(),
		HostID:             hostID,
		Title:              title,
		Description:        strings.TrimSpace(description),
		Type:               pType,
		Status:             StatusDraft,
		Address:            address,
		PricePerNight:      price,
		CleaningFee:        cleaningFee,
		MaxGuests:          maxGuests,
		Bedrooms:           bedrooms,
		Beds:               beds,
		Bathrooms:          bathrooms,
		Amenities:          NormalizeAmenities(amenities),
		Photos:             []Photo{},
		CancellationPolicy: PolicyFlexible,
		MinNights:          1,
		MaxNights:          0,
		GuestsIncluded:     maxGuests,
		ExtraGuestFee:      zeroMoney(price.Currency()),
		SecurityDeposit:    zeroMoney(price.Currency()),
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func zeroMoney(currency string) shared.Money {
	m, _ := shared.NewMoney(0, currency)
	return m
}

// SetStayRules configures the listing's stay-length limits and per-guest pricing.
// minNights >= 1; maxNights is 0 (no cap) or >= minNights; guestsIncluded is
// 1..MaxGuests; the fee/deposit must share the nightly price's currency.
func (p *Property) SetStayRules(minNights, maxNights, guestsIncluded int, extraGuestFee, securityDeposit shared.Money) error {
	if minNights < 1 {
		return shared.NewValidationError("minimum nights must be at least 1")
	}
	if maxNights != 0 && maxNights < minNights {
		return shared.NewValidationError("maximum nights must be 0 or at least the minimum")
	}
	if guestsIncluded < 1 || guestsIncluded > p.MaxGuests {
		return shared.NewValidationError("guests included must be between 1 and the maximum guests")
	}
	cur := p.PricePerNight.Currency()
	if extraGuestFee.Currency() != cur || securityDeposit.Currency() != cur {
		return shared.NewValidationError("fees must use the same currency as the nightly price")
	}
	if extraGuestFee.AmountCents() < 0 || securityDeposit.AmountCents() < 0 {
		return shared.NewValidationError("fees cannot be negative")
	}
	p.MinNights = minNights
	p.MaxNights = maxNights
	p.GuestsIncluded = guestsIncluded
	p.ExtraGuestFee = extraGuestFee
	p.SecurityDeposit = securityDeposit
	p.touch()
	return nil
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

// RemovePhoto detaches a photo from the listing and renumbers the rest so
// positions stay contiguous from zero (the first photo is the cover).
func (p *Property) RemovePhoto(photoID uuid.UUID) error {
	idx := -1
	for i, ph := range p.Photos {
		if ph.ID == photoID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return shared.ErrNotFound
	}
	p.Photos = append(p.Photos[:idx], p.Photos[idx+1:]...)
	p.renumberPhotos()
	p.touch()
	return nil
}

// ReorderPhotos rearranges photos to match orderedIDs, which must be a
// permutation of the listing's current photo IDs. Position 0 becomes the cover.
func (p *Property) ReorderPhotos(orderedIDs []uuid.UUID) error {
	if len(orderedIDs) != len(p.Photos) {
		return shared.NewValidationError("the order must list every photo exactly once")
	}
	byID := make(map[uuid.UUID]Photo, len(p.Photos))
	for _, ph := range p.Photos {
		byID[ph.ID] = ph
	}
	reordered := make([]Photo, 0, len(orderedIDs))
	seen := make(map[uuid.UUID]struct{}, len(orderedIDs))
	for _, id := range orderedIDs {
		ph, ok := byID[id]
		if !ok {
			return shared.NewValidationError("unknown photo in the requested order")
		}
		if _, dup := seen[id]; dup {
			return shared.NewValidationError("a photo is listed more than once")
		}
		seen[id] = struct{}{}
		reordered = append(reordered, ph)
	}
	p.Photos = reordered
	p.renumberPhotos()
	p.touch()
	return nil
}

func (p *Property) renumberPhotos() {
	for i := range p.Photos {
		p.Photos[i].Position = i
	}
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
