package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/airhost/backend/internal/domain/idempotency"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IdempotencyRepository is the Postgres implementation of
// idempotency.Repository, backing the RFC-style Idempotency-Key
// middleware (S160). The table is keyed on (user_id, key) so one
// user's namespace cannot leak another user's captured response.
type IdempotencyRepository struct {
	pool *pgxpool.Pool
}

// NewIdempotencyRepository builds an IdempotencyRepository.
func NewIdempotencyRepository(pool *pgxpool.Pool) *IdempotencyRepository {
	return &IdempotencyRepository{pool: pool}
}

const idempotencyColumns = `key, user_id, method, path, body_hash, status_code, response_body, response_content_type, created_at`

func (r *IdempotencyRepository) Get(ctx context.Context, userID uuid.UUID, key string) (*idempotency.Record, error) {
	rec := &idempotency.Record{}
	err := r.pool.QueryRow(ctx,
		`SELECT `+idempotencyColumns+` FROM request_idempotency WHERE user_id=$1 AND key=$2`,
		userID, key,
	).Scan(&rec.Key, &rec.UserID, &rec.Method, &rec.Path, &rec.BodyHash,
		&rec.StatusCode, &rec.ResponseBody, &rec.ResponseContentType, &rec.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.ErrNotFound
		}
		return nil, mapError(err)
	}
	return rec, nil
}

// Put inserts a record under (user_id, key). On a conflict (a race
// between two concurrent stores under the same composite key) the
// first writer wins and we silently skip — the lookup the middleware
// already ran said there was no record yet, so it's safe to surface
// success either way.
func (r *IdempotencyRepository) Put(ctx context.Context, rec *idempotency.Record) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO request_idempotency (`+idempotencyColumns+`)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT (user_id, key) DO NOTHING`,
		rec.Key, rec.UserID, rec.Method, rec.Path, rec.BodyHash,
		rec.StatusCode, rec.ResponseBody, rec.ResponseContentType, rec.CreatedAt,
	)
	return mapError(err)
}

// Cleanup drops records created at or before the cutoff. The boundary
// is inclusive so a sweep with a "now-24h" cutoff drops everything
// already aged out even when timestamps tie at the clock resolution.
func (r *IdempotencyRepository) Cleanup(ctx context.Context, olderThan time.Time) (int64, error) {
	ct, err := r.pool.Exec(ctx, `DELETE FROM request_idempotency WHERE created_at <= $1`, olderThan)
	if err != nil {
		return 0, mapError(err)
	}
	return ct.RowsAffected(), nil
}

var _ idempotency.Repository = (*IdempotencyRepository)(nil)
