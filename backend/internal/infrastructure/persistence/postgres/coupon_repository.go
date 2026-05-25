package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/coupon"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CouponRepository is the Postgres implementation of coupon.Repository.
type CouponRepository struct {
	pool *pgxpool.Pool
}

// NewCouponRepository builds a CouponRepository.
func NewCouponRepository(pool *pgxpool.Pool) *CouponRepository {
	return &CouponRepository{pool: pool}
}

const couponColumns = `id, code, kind, percent, amount_cents, currency, min_nights, max_redemptions, redemptions, expires_at, active, created_at`

func (r *CouponRepository) Create(ctx context.Context, c *coupon.Coupon) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO coupons (`+couponColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		c.ID, c.Code, string(c.Kind), c.Percent, c.AmountCents, c.Currency,
		c.MinNights, c.MaxRedemptions, c.Redemptions, c.ExpiresAt, c.Active, c.CreatedAt,
	)
	return mapError(err)
}

func (r *CouponRepository) Update(ctx context.Context, c *coupon.Coupon) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE coupons SET redemptions=$2, active=$3 WHERE id=$1`,
		c.ID, c.Redemptions, c.Active,
	)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *CouponRepository) FindByID(ctx context.Context, id uuid.UUID) (*coupon.Coupon, error) {
	c, err := scanCoupon(r.pool.QueryRow(ctx, `SELECT `+couponColumns+` FROM coupons WHERE id=$1`, id))
	if err != nil {
		return nil, mapError(err)
	}
	return c, nil
}

func (r *CouponRepository) FindByCode(ctx context.Context, code string) (*coupon.Coupon, error) {
	c, err := scanCoupon(r.pool.QueryRow(ctx,
		`SELECT `+couponColumns+` FROM coupons WHERE upper(code)=upper($1)`, coupon.NormalizeCode(code)))
	if err != nil {
		return nil, mapError(err)
	}
	return c, nil
}

func (r *CouponRepository) List(ctx context.Context, page shared.Page) (shared.PageResult[*coupon.Coupon], error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM coupons`).Scan(&total); err != nil {
		return shared.PageResult[*coupon.Coupon]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+couponColumns+` FROM coupons ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`,
		page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*coupon.Coupon]{}, mapError(err)
	}
	defer rows.Close()

	var items []*coupon.Coupon
	for rows.Next() {
		c, err := scanCoupon(rows)
		if err != nil {
			return shared.PageResult[*coupon.Coupon]{}, mapError(err)
		}
		items = append(items, c)
	}
	return shared.PageResult[*coupon.Coupon]{Items: items, Total: total}, mapError(rows.Err())
}

func scanCoupon(row rowScanner) (*coupon.Coupon, error) {
	var (
		c    coupon.Coupon
		kind string
	)
	err := row.Scan(&c.ID, &c.Code, &kind, &c.Percent, &c.AmountCents, &c.Currency,
		&c.MinNights, &c.MaxRedemptions, &c.Redemptions, &c.ExpiresAt, &c.Active, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.Kind = coupon.Kind(kind)
	return &c, nil
}
