package savedsearch

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository is the persistence port for the SavedSearch aggregate.
type Repository interface {
	Create(ctx context.Context, s *SavedSearch) error
	// ListByUser returns a user's saved searches, newest first.
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*SavedSearch, error)
	// Delete removes a saved search the user owns.
	Delete(ctx context.Context, userID, id uuid.UUID) error
	// SetAlerts toggles new-listing alerts for a saved search the user owns.
	SetAlerts(ctx context.Context, userID, id uuid.UUID, enabled bool) error
	// ListAlertable returns every saved search with alerts enabled (across users),
	// driving the periodic alert job.
	ListAlertable(ctx context.Context) ([]*SavedSearch, error)
	// MarkNotified advances the alert watermark after notifying a user.
	MarkNotified(ctx context.Context, id uuid.UUID, at time.Time) error
	// EraseByUser hard-deletes every saved search owned by the user — the
	// GDPR right-to-erasure path. Saved searches are pure preference data
	// with no retention basis, so they are dropped rather than anonymised.
	// Returns the number of rows removed.
	EraseByUser(ctx context.Context, userID uuid.UUID) (int, error)
}
