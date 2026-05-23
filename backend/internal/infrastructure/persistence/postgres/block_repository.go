package postgres

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/block"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BlockRepository is the Postgres implementation of block.Repository.
type BlockRepository struct {
	pool *pgxpool.Pool
}

// NewBlockRepository builds a BlockRepository.
func NewBlockRepository(pool *pgxpool.Pool) *BlockRepository {
	return &BlockRepository{pool: pool}
}

const blockColumns = `id, property_id, check_in, check_out, reason, created_at`

func (r *BlockRepository) Create(ctx context.Context, b *block.Block) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO property_blocks (`+blockColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		b.ID, b.PropertyID, b.Dates.CheckIn, b.Dates.CheckOut, b.Reason, b.CreatedAt,
	)
	return mapError(err)
}

func (r *BlockRepository) Delete(ctx context.Context, id uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM property_blocks WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *BlockRepository) FindByID(ctx context.Context, id uuid.UUID) (*block.Block, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+blockColumns+` FROM property_blocks WHERE id=$1`, id)
	return scanBlock(row)
}

func (r *BlockRepository) ListByProperty(ctx context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*block.Block], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM property_blocks WHERE property_id=$1`, propertyID,
	).Scan(&total); err != nil {
		return shared.PageResult[*block.Block]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+blockColumns+` FROM property_blocks WHERE property_id=$1
		ORDER BY check_in ASC LIMIT $2 OFFSET $3`,
		propertyID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*block.Block]{}, mapError(err)
	}
	defer rows.Close()

	var items []*block.Block
	for rows.Next() {
		b, err := scanBlock(rows)
		if err != nil {
			return shared.PageResult[*block.Block]{}, err
		}
		items = append(items, b)
	}
	return shared.PageResult[*block.Block]{Items: items, Total: total}, mapError(rows.Err())
}

func (r *BlockRepository) HasOverlap(ctx context.Context, propertyID uuid.UUID, dates booking.DateRange) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM property_blocks
			WHERE property_id=$1 AND check_in < $3 AND $2 < check_out
		)`, propertyID, dates.CheckIn, dates.CheckOut,
	).Scan(&exists)
	return exists, mapError(err)
}

func (r *BlockRepository) ListInRange(ctx context.Context, propertyID uuid.UUID, from, to time.Time) ([]*block.Block, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+blockColumns+` FROM property_blocks
		WHERE property_id=$1 AND check_in < $3 AND $2 < check_out
		ORDER BY check_in ASC`,
		propertyID, from, to,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var items []*block.Block
	for rows.Next() {
		b, err := scanBlock(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return items, mapError(rows.Err())
}

func (r *BlockRepository) BlockedPropertyIDs(ctx context.Context, from, to time.Time) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT property_id FROM property_blocks
		WHERE check_in < $2 AND $1 < check_out`, from, to,
	)
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

func scanBlock(row rowScanner) (*block.Block, error) {
	var (
		b  block.Block
		ci time.Time
		co time.Time
	)
	if err := row.Scan(&b.ID, &b.PropertyID, &ci, &co, &b.Reason, &b.CreatedAt); err != nil {
		return nil, mapError(err)
	}
	b.Dates = booking.DateRange{CheckIn: ci, CheckOut: co}
	return &b, nil
}
