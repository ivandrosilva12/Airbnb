package memory

import (
	"context"
	"sync"
	"time"

	"github.com/airhost/backend/internal/application/port"
)

// WebhookEventRepository is an in-memory port.WebhookDedupeStore: it remembers
// which (provider, eventID) deliveries have been processed, with timestamps so
// retention cleanup can drop old entries.
type WebhookEventRepository struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// NewWebhookEventRepository builds an empty in-memory dedupe store.
func NewWebhookEventRepository() *WebhookEventRepository {
	return &WebhookEventRepository{seen: map[string]time.Time{}}
}

func dedupeKey(provider, eventID string) string { return provider + ":" + eventID }

func (r *WebhookEventRepository) Seen(_ context.Context, provider, eventID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.seen[dedupeKey(provider, eventID)]
	return ok, nil
}

func (r *WebhookEventRepository) Record(_ context.Context, provider, eventID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen[dedupeKey(provider, eventID)] = time.Now().UTC()
	return nil
}

// DeleteOlderThan prunes records recorded at or before the cutoff. The boundary
// is inclusive (mirroring the Postgres store) so a sweep with a "now" cutoff
// drops everything already recorded even when timestamps coincide with the
// cutoff at the clock's resolution.
func (r *WebhookEventRepository) DeleteOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var deleted int64
	for k, ts := range r.seen {
		if !ts.After(cutoff) {
			delete(r.seen, k)
			deleted++
		}
	}
	return deleted, nil
}

var _ port.WebhookDedupeStore = (*WebhookEventRepository)(nil)
