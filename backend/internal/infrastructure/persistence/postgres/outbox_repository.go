package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
		 WHERE processed_at IS NULL AND failed_at IS NULL AND dead_lettered_at IS NULL
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

// IncrementAttempt bumps the attempt counter and returns the new value (S97).
// Used by the relay before each delivery so the budget is charged whether the
// dispatcher returned, panicked, or the process crashed mid-flight.
func (r *OutboxRepository) IncrementAttempt(ctx context.Context, id uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`UPDATE outbox SET attempts = attempts + 1 WHERE id = $1 RETURNING attempts`, id,
	).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, mapError(err)
	}
	return n, nil
}

// DeadLetter promotes a record to the dead-letter state with a reason.
// Idempotent on retry — multiple calls only update the timestamp / reason.
func (r *OutboxRepository) DeadLetter(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE outbox SET dead_lettered_at = COALESCE(dead_lettered_at, now()),
		                   last_error = $2
		   WHERE id = $1`, id, reason,
	)
	return mapError(err)
}

// Requeue clears the dead-letter state and the attempt counter, so the next
// recovery cycle picks the record up again. failed_at is also cleared in case
// it was set by an older code path.
func (r *OutboxRepository) Requeue(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE outbox SET dead_lettered_at = NULL, last_error = NULL,
		                   failed_at = NULL, attempts = 0
		   WHERE id = $1`, id,
	)
	return mapError(err)
}

// ListDeadLettered returns dead-lettered records ordered by when they were
// dead-lettered (newest first), paginated.
func (r *OutboxRepository) ListDeadLettered(ctx context.Context, limit, offset int) ([]event.DeadLetteredRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, event_name, payload, created_at, attempts,
		        dead_lettered_at, COALESCE(last_error, '')
		   FROM outbox
		  WHERE dead_lettered_at IS NOT NULL
		  ORDER BY dead_lettered_at DESC
		  LIMIT $1 OFFSET $2`, limit, offset,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []event.DeadLetteredRecord
	for rows.Next() {
		var dl event.DeadLetteredRecord
		var dlAt time.Time
		if err := rows.Scan(&dl.Record.ID, &dl.Record.Name, &dl.Record.Payload, &dl.Record.CreatedAt,
			&dl.Record.Attempts, &dlAt, &dl.LastError); err != nil {
			return nil, mapError(err)
		}
		dl.DeadLetteredAt = dlAt
		out = append(out, dl)
	}
	return out, mapError(rows.Err())
}

// PendingCount returns how many records are still awaiting delivery — what the
// recovery scan would pick up. Feeds the Prometheus depth gauge.
func (r *OutboxRepository) PendingCount(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM outbox
		  WHERE processed_at IS NULL AND failed_at IS NULL AND dead_lettered_at IS NULL`,
	).Scan(&n)
	if err != nil {
		return 0, mapError(err)
	}
	return n, nil
}

var _ event.OutboxStore = (*OutboxRepository)(nil)
var _ event.DeadLetterStore = (*OutboxRepository)(nil)
