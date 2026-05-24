package property

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// GeoFilter restricts results to listings within RadiusKm of a point.
type GeoFilter struct {
	Lat      float64
	Lng      float64
	RadiusKm float64
}

// Sort selects the ordering of search results.
type Sort string

const (
	SortNewest    Sort = "newest" // default: most recently created first
	SortPriceAsc  Sort = "price_asc"
	SortPriceDesc Sort = "price_desc"
	SortRating    Sort = "rating" // highest average rating first
)

// SearchCriteria expresses the filters available when searching listings.
type SearchCriteria struct {
	// Query is a free-text term matched (case-insensitively) against the
	// listing title, description and city. Empty means no text filter.
	Query     string
	City      string
	Country   string
	Type      PropertyType
	MinGuests int
	MaxPrice  int64 // in cents; 0 means no cap
	Amenities []string
	// Geo, when non-nil, keeps only listings within the given radius of a point.
	Geo *GeoFilter
	// Sort selects the result ordering (defaults to newest).
	Sort Sort
	// ExcludeIDs removes specific listings from the results (e.g. those already
	// booked for a requested date range). Empty means no exclusion.
	ExcludeIDs []uuid.UUID
	Page       shared.Page
}

// Repository is the persistence port for the Property aggregate.
type Repository interface {
	Create(ctx context.Context, p *Property) error
	Update(ctx context.Context, p *Property) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindByID(ctx context.Context, id uuid.UUID) (*Property, error)
	// FindByIDs batch-loads properties by id (order unspecified); missing ids are
	// simply absent from the result. Used to hydrate lists without an N+1.
	FindByIDs(ctx context.Context, ids []uuid.UUID) ([]*Property, error)
	ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*Property], error)
	Search(ctx context.Context, criteria SearchCriteria) (shared.PageResult[*Property], error)
	// UpdateRating persists the denormalised review aggregates for a listing.
	UpdateRating(ctx context.Context, id uuid.UUID, average float64, count int) error
}

// AvailabilitySearcher is an optional Repository capability: it runs a search
// while excluding listings that are booked or host-blocked over [checkIn,
// checkOut), filtering inside the data store rather than having the caller
// materialise the occupied id set. The search service prefers this push-down
// when the backing repository implements it, falling back to ExcludeIDs
// otherwise.
type AvailabilitySearcher interface {
	SearchAvailable(ctx context.Context, criteria SearchCriteria, checkIn, checkOut time.Time) (shared.PageResult[*Property], error)
}
