package postgres

import (
	"context"

	"github.com/airhost/backend/internal/application/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WebhookEventRepository is the Postgres implementation of
// port.WebhookDedupeStore, recording processed (provider, event_id) deliveries
// so duplicates can be skipped.
type WebhookEventRepository struct {
	pool *pgxpool.Pool
}

// NewWebhookEventRepository builds a WebhookEventRepository.
func NewWebhookEventRepository(pool *pgxpool.Pool) *WebhookEventRepository {
	return &WebhookEventRepository{pool: pool}
}

func (r *WebhookEventRepository) Seen(ctx context.Context, provider, eventID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM webhook_events WHERE provider=$1 AND event_id=$2)`,
		provider, eventID,
	).Scan(&exists)
	return exists, mapError(err)
}

func (r *WebhookEventRepository) Record(ctx context.Context, provider, eventID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO webhook_events (provider, event_id, created_at) VALUES ($1,$2,now())
		 ON CONFLICT (provider, event_id) DO NOTHING`,
		provider, eventID,
	)
	return mapError(err)
}

var _ port.WebhookDedupeStore = (*WebhookEventRepository)(nil)
