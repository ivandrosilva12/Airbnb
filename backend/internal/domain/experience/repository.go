package experience

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// SearchFilter narrows the public catalogue list. All fields are
// optional; the zero filter returns every published experience.
type SearchFilter struct {
	Category Category // optional — empty matches any
	City     string   // optional substring (case-insensitive)
	Country  string   // optional substring (case-insensitive)
	Language string   // optional — exact ISO code match
}

// Repository is the persistence port for the Experience aggregate.
type Repository interface {
	Create(ctx context.Context, e *Experience) error
	Update(ctx context.Context, e *Experience) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindByID(ctx context.Context, id uuid.UUID) (*Experience, error)
	// ListByHost returns every listing owned by the host (any status),
	// newest first. Used by the host dashboard.
	ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*Experience], error)
	// Search returns published listings only, filtered by the SearchFilter.
	// Used by the public catalogue.
	Search(ctx context.Context, f SearchFilter, page shared.Page) (shared.PageResult[*Experience], error)
}
