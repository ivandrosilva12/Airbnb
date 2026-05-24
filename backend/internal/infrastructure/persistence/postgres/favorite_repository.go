package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/favorite"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FavoriteRepository is the Postgres implementation of favorite.Repository.
type FavoriteRepository struct {
	pool *pgxpool.Pool
}

// NewFavoriteRepository builds a FavoriteRepository.
func NewFavoriteRepository(pool *pgxpool.Pool) *FavoriteRepository {
	return &FavoriteRepository{pool: pool}
}

func (r *FavoriteRepository) Add(ctx context.Context, f *favorite.Favorite) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO favorites (user_id, property_id, created_at)
		VALUES ($1,$2,$3)
		ON CONFLICT (user_id, property_id) DO NOTHING`,
		f.UserID, f.PropertyID, f.CreatedAt,
	)
	return mapError(err)
}

func (r *FavoriteRepository) Remove(ctx context.Context, userID, propertyID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM favorites WHERE user_id=$1 AND property_id=$2`, userID, propertyID)
	return mapError(err)
}

func (r *FavoriteRepository) Exists(ctx context.Context, userID, propertyID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM favorites WHERE user_id=$1 AND property_id=$2)`,
		userID, propertyID,
	).Scan(&exists)
	return exists, mapError(err)
}

func (r *FavoriteRepository) ListPropertyIDs(ctx context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[uuid.UUID], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM favorites WHERE user_id=$1`, userID,
	).Scan(&total); err != nil {
		return shared.PageResult[uuid.UUID]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT property_id FROM favorites WHERE user_id=$1
		ORDER BY created_at DESC, property_id DESC LIMIT $2 OFFSET $3`,
		userID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[uuid.UUID]{}, mapError(err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return shared.PageResult[uuid.UUID]{}, mapError(err)
		}
		ids = append(ids, id)
	}
	return shared.PageResult[uuid.UUID]{Items: ids, Total: total}, mapError(rows.Err())
}
