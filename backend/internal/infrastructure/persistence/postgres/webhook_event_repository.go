package postgres

import (
	"context"
	"time"

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

// DeleteOlderThan prunes records recorded at or before the cutoff. The boundary
// is inclusive so a retention sweep with a "now" cutoff (olderThanDays=0) drops
// everything already recorded, even when a record's timestamp coincides with the
// cutoff to the resolution of the clock.
func (r *WebhookEventRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	ct, err := r.pool.Exec(ctx, `DELETE FROM webhook_events WHERE created_at <= $1`, cutoff)
	if err != nil {
		return 0, mapError(err)
	}
	return ct.RowsAffected(), nil
}

var _ port.WebhookDedupeStore = (*WebhookEventRepository)(nil)
