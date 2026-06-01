package pricerule

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository is the persistence port for the Rule aggregate.
type Repository interface {
	Create(ctx context.Context, r *Rule) error
	// Delete removes a rule belonging to the given property. Returns ErrNotFound
	// when the rule does not exist or belongs to a different property.
	Delete(ctx context.Context, propertyID, id uuid.UUID) error
	// ListByProperty returns every rule attached to a listing, soonest first.
	ListByProperty(ctx context.Context, propertyID uuid.UUID) ([]*Rule, error)
	// ListOverlapping returns the rules that touch the half-open [start, end)
	// window for a property — used at booking time to compute the subtotal.
	ListOverlapping(ctx context.Context, propertyID uuid.UUID, start, end time.Time) ([]*Rule, error)
}
