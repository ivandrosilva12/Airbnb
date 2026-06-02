package tax

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository persists Rule rows. List is used by the admin UI;
// RulesFor is the hot path the calculator hits for each quote, so
// implementations are expected to pre-filter by country/city at the
// storage layer rather than returning everything and filtering in Go.
type Repository interface {
	// Save inserts or replaces a rule by ID. The admin flow creates
	// rules via NewRule (fresh UUID) so the upsert path is exercised
	// only during e2e seeding.
	Save(ctx context.Context, rule *Rule) error
	// Delete removes a rule by ID. Idempotent: deleting a missing rule
	// returns nil.
	Delete(ctx context.Context, id uuid.UUID) error
	// List returns every rule, name-sorted, for the admin console.
	List(ctx context.Context) ([]*Rule, error)
	// RulesFor returns the rules whose jurisdiction matches the
	// (country, city) tuple AND whose effective window covers `at`.
	// Implementations may over-return (e.g. ignore the date window in
	// SQL and let the calculator's Matches filter the rest) — the
	// contract is "at least every applicable rule".
	RulesFor(ctx context.Context, country, city string, at time.Time) ([]*Rule, error)
}
