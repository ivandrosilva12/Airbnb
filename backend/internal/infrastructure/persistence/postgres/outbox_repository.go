package postgres

import (
	"context"

	"github.com/airhost/backend/internal/application/event"
	"github.com/google/uuid"
)

// OutboxRepository is the Postgres implementation of event.OutboxStore. It runs
// against a querier (the pool, or a transaction inside a UnitOfWork) so an event
// can be appended in the same transaction as the domain write that produced it.
type OutboxRepository struct {
	pool querier
}

// NewOutboxRepository builds an OutboxRepository.
func NewOutboxRepository(db querier) *OutboxRepository {
	return &OutboxRepository{pool: db}
}

func (r *OutboxRepository) Append(ctx context.Context, rec event.Record) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO outbox (id, event_name, payload, created_at, attempts)
		 VALUES ($1, $2, $3::jsonb, $4, $5)`,
		rec.ID, rec.Name, string(rec.Payload), rec.CreatedAt, rec.Attempts,
	)
	return mapError(err)
}

func (r *OutboxRepository) FetchUnprocessed(ctx context.Context, limit int) ([]event.Record, error) {
	if limit <= 0 {
		limit = 100
	}
	// Only consider records older than a short grace period: a freshly-committed
	// event is dispatched synchronously by its own unit of work, so the recovery
	// relay should pick up only events genuinely left behind, avoiding a race that
	// would re-deliver an event a unit of work is still dispatching.
	rows, err := r.pool.Query(ctx,
		`SELECT id, event_name, payload, created_at, attempts
		 FROM outbox
		 WHERE processed_at IS NULL AND failed_at IS NULL
		   AND created_at < now() - interval '30 seconds'
		 ORDER BY created_at ASC
		 LIMIT $1`, limit,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []event.Record
	for rows.Next() {
		var rec event.Record
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Payload, &rec.CreatedAt, &rec.Attempts); err != nil {
			return nil, mapError(err)
		}
		out = append(out, rec)
	}
	return out, mapError(rows.Err())
}

func (r *OutboxRepository) MarkProcessed(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox SET processed_at = now() WHERE id = $1`, id)
	return mapError(err)
}

func (r *OutboxRepository) MarkFailed(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox SET failed_at = now(), attempts = attempts + 1 WHERE id = $1`, id)
	return mapError(err)
}

var _ event.OutboxStore = (*OutboxRepository)(nil)
