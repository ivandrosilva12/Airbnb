package property

import (
	"context"

	"github.com/google/uuid"
)

// CohostRepository is the persistence port for the Cohost grant. Implementations
// live in the infrastructure layer.
//
// Uniqueness: a given (propertyID, userID) pair may appear at most once. The
// application layer relies on FindByPropertyAndUser to detect an existing
// grant before issuing Create, and on the database UNIQUE constraint as the
// race-safe backstop.
type CohostRepository interface {
	Create(ctx context.Context, c *Cohost) error
	Update(ctx context.Context, c *Cohost) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindByID(ctx context.Context, id uuid.UUID) (*Cohost, error)
	// FindByPropertyAndUser returns the grant a user has on a listing, or
	// shared.ErrNotFound if no such grant exists.
	FindByPropertyAndUser(ctx context.Context, propertyID, userID uuid.UUID) (*Cohost, error)
	// ListByProperty returns every co-host on a listing, sorted by CreatedAt
	// ascending. There is intentionally no pagination — the number of
	// co-hosts on a single listing is small by design.
	ListByProperty(ctx context.Context, propertyID uuid.UUID) ([]*Cohost, error)
	// ListPropertiesForUser returns the property IDs on which the user has a
	// grant. Used to surface "listings I help manage" without an N+1.
	ListPropertiesForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}
