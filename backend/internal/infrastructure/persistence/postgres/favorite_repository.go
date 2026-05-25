package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/favorite"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
		INSERT INTO favorites (user_id, property_id, collection_id, created_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id, property_id) DO UPDATE SET collection_id = EXCLUDED.collection_id`,
		f.UserID, f.PropertyID, f.CollectionID, f.CreatedAt,
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

func (r *FavoriteRepository) SetCollection(ctx context.Context, userID, propertyID uuid.UUID, collectionID *uuid.UUID) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE favorites SET collection_id=$3 WHERE user_id=$1 AND property_id=$2`,
		userID, propertyID, collectionID)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
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
	return scanPropertyIDs(rows, total)
}

func (r *FavoriteRepository) ListPropertyIDsInCollection(ctx context.Context, userID uuid.UUID, collectionID *uuid.UUID, page shared.Page) (shared.PageResult[uuid.UUID], error) {
	// A nil collectionID selects the default bucket (collection_id IS NULL); the
	// "IS NOT DISTINCT FROM" predicate matches both NULL and a concrete id.
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM favorites WHERE user_id=$1 AND collection_id IS NOT DISTINCT FROM $2`,
		userID, collectionID,
	).Scan(&total); err != nil {
		return shared.PageResult[uuid.UUID]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT property_id FROM favorites WHERE user_id=$1 AND collection_id IS NOT DISTINCT FROM $2
		ORDER BY created_at DESC, property_id DESC LIMIT $3 OFFSET $4`,
		userID, collectionID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[uuid.UUID]{}, mapError(err)
	}
	return scanPropertyIDs(rows, total)
}

func scanPropertyIDs(rows pgx.Rows, total int64) (shared.PageResult[uuid.UUID], error) {
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

func (r *FavoriteRepository) CreateCollection(ctx context.Context, c *favorite.Collection) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO wishlist_collections (id, user_id, name, created_at)
		VALUES ($1,$2,$3,$4)`,
		c.ID, c.UserID, c.Name, c.CreatedAt,
	)
	return mapError(err)
}

func (r *FavoriteRepository) DeleteCollection(ctx context.Context, userID, collectionID uuid.UUID) error {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM wishlist_collections WHERE id=$1 AND user_id=$2`, collectionID, userID)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *FavoriteRepository) ListCollections(ctx context.Context, userID uuid.UUID) ([]favorite.CollectionWithCount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.user_id, c.name, c.created_at, count(f.property_id)
		FROM wishlist_collections c
		LEFT JOIN favorites f ON f.collection_id = c.id
		WHERE c.user_id=$1
		GROUP BY c.id
		ORDER BY c.created_at DESC`, userID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	out := make([]favorite.CollectionWithCount, 0)
	for rows.Next() {
		var (
			c     favorite.Collection
			count int
		)
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.CreatedAt, &count); err != nil {
			return nil, mapError(err)
		}
		cc := c
		out = append(out, favorite.CollectionWithCount{Collection: &cc, Count: count})
	}
	return out, mapError(rows.Err())
}

func (r *FavoriteRepository) FindCollection(ctx context.Context, userID, collectionID uuid.UUID) (*favorite.Collection, error) {
	var c favorite.Collection
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, name, created_at FROM wishlist_collections WHERE id=$1 AND user_id=$2`,
		collectionID, userID,
	).Scan(&c.ID, &c.UserID, &c.Name, &c.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &c, nil
}
