package property

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// SearchCriteria expresses the filters available when searching listings.
type SearchCriteria struct {
	City      string
	Country   string
	Type      PropertyType
	MinGuests int
	MaxPrice  int64 // in cents; 0 means no cap
	Amenities []string
	Page      shared.Page
}

// Repository is the persistence port for the Property aggregate.
type Repository interface {
	Create(ctx context.Context, p *Property) error
	Update(ctx context.Context, p *Property) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindByID(ctx context.Context, id uuid.UUID) (*Property, error)
	ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*Property], error)
	Search(ctx context.Context, criteria SearchCriteria) (shared.PageResult[*Property], error)
}
