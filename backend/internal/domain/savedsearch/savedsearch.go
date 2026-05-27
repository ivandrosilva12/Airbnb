// Package savedsearch is the bounded context for a guest's saved searches and
// their optional new-listing alerts. A SavedSearch stores the search criteria
// (as the raw URL query the client used) so it can be replayed, and a watermark
// of when the user was last alerted about matching listings.
package savedsearch

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

const (
	maxNameLen  = 80
	maxQueryLen = 2000
)

// SavedSearch is the aggregate root for a user's saved search.
type SavedSearch struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Name          string
	Query         string // raw URL query, e.g. "city=Lisbon&minPrice=5000&instantBook=true"
	AlertsEnabled bool
	// LastNotifiedAt is the watermark: only listings created after it trigger a
	// new alert. Initialised to creation time so pre-existing listings don't all
	// fire at once.
	LastNotifiedAt time.Time
	CreatedAt      time.Time
}

// New creates a saved search, validating its name and stored query.
func New(userID uuid.UUID, name, query string, alertsEnabled bool) (*SavedSearch, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, shared.NewValidationError("a name is required")
	}
	if len([]rune(name)) > maxNameLen {
		return nil, shared.NewValidationError("name is too long")
	}
	query = strings.TrimPrefix(strings.TrimSpace(query), "?")
	if len(query) > maxQueryLen {
		return nil, shared.NewValidationError("search is too long")
	}
	now := time.Now().UTC()
	return &SavedSearch{
		ID:             uuid.New(),
		UserID:         userID,
		Name:           name,
		Query:          query,
		AlertsEnabled:  alertsEnabled,
		LastNotifiedAt: now,
		CreatedAt:      now,
	}, nil
}
