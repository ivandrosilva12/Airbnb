package memory

import (
	"context"
	"sync"

	"github.com/airhost/backend/internal/application/port"
)

// WebhookEventRepository is an in-memory port.WebhookDedupeStore: it remembers
// which (provider, eventID) deliveries have been processed.
type WebhookEventRepository struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// NewWebhookEventRepository builds an empty in-memory dedupe store.
func NewWebhookEventRepository() *WebhookEventRepository {
	return &WebhookEventRepository{seen: map[string]struct{}{}}
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
	r.seen[dedupeKey(provider, eventID)] = struct{}{}
	return nil
}

var _ port.WebhookDedupeStore = (*WebhookEventRepository)(nil)
