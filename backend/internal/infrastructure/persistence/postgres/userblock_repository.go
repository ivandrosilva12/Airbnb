package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/userblock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserBlockRepository is the Postgres implementation of userblock.Repository.
type UserBlockRepository struct {
	pool *pgxpool.Pool
}

// NewUserBlockRepository builds a UserBlockRepository.
func NewUserBlockRepository(pool *pgxpool.Pool) *UserBlockRepository {
	return &UserBlockRepository{pool: pool}
}

var _ userblock.Repository = (*UserBlockRepository)(nil)

func (r *UserBlockRepository) Add(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_blocks (blocker_id, blocked_id) VALUES ($1,$2)
		 ON CONFLICT (blocker_id, blocked_id) DO NOTHING`,
		blockerID, blockedID,
	)
	return mapError(err)
}

func (r *UserBlockRepository) Remove(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_blocks WHERE blocker_id=$1 AND blocked_id=$2`, blockerID, blockedID)
	return mapError(err)
}

func (r *UserBlockRepository) ListBlocked(ctx context.Context, blockerID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `SELECT blocked_id FROM user_blocks WHERE blocker_id=$1 ORDER BY created_at DESC`, blockerID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, mapError(err)
		}
		ids = append(ids, id)
	}
	return ids, mapError(rows.Err())
}

func (r *UserBlockRepository) IsBlocked(ctx context.Context, a, b uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM user_blocks
			WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1)
		)`, a, b,
	).Scan(&exists)
	return exists, mapError(err)
}
