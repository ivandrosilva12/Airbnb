package postgres

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/pricerule"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PriceRuleRepository is the Postgres implementation of pricerule.Repository.
type PriceRuleRepository struct {
	pool *pgxpool.Pool
}

// NewPriceRuleRepository builds a PriceRuleRepository.
func NewPriceRuleRepository(pool *pgxpool.Pool) *PriceRuleRepository {
	return &PriceRuleRepository{pool: pool}
}

var _ pricerule.Repository = (*PriceRuleRepository)(nil)

const priceRuleColumns = `id, property_id, start_date, end_date, price_cents, currency, label, created_at`

func (r *PriceRuleRepository) Create(ctx context.Context, rule *pricerule.Rule) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO price_rules (`+priceRuleColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		rule.ID, rule.PropertyID, rule.StartDate, rule.EndDate,
		rule.PriceCents, rule.Currency, rule.Label, rule.CreatedAt,
	)
	return mapError(err)
}

func (r *PriceRuleRepository) Delete(ctx context.Context, propertyID, id uuid.UUID) error {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM price_rules WHERE id=$1 AND property_id=$2`, id, propertyID)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *PriceRuleRepository) ListByProperty(ctx context.Context, propertyID uuid.UUID) ([]*pricerule.Rule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+priceRuleColumns+` FROM price_rules WHERE property_id=$1 ORDER BY start_date`, propertyID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanPriceRules(rows)
}

func (r *PriceRuleRepository) ListOverlapping(ctx context.Context, propertyID uuid.UUID, start, end time.Time) ([]*pricerule.Rule, error) {
	// Half-open overlap: existing.start < new.end AND new.start < existing.end.
	rows, err := r.pool.Query(ctx, `
		SELECT `+priceRuleColumns+` FROM price_rules
		WHERE property_id=$1 AND start_date < $3 AND $2 < end_date
		ORDER BY start_date`, propertyID, start, end)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanPriceRules(rows)
}

func scanPriceRules(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*pricerule.Rule, error) {
	var out []*pricerule.Rule
	for rows.Next() {
		var rule pricerule.Rule
		if err := rows.Scan(&rule.ID, &rule.PropertyID, &rule.StartDate, &rule.EndDate,
			&rule.PriceCents, &rule.Currency, &rule.Label, &rule.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		out = append(out, &rule)
	}
	return out, mapError(rows.Err())
}
