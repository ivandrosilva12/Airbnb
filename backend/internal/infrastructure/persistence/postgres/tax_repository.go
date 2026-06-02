package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/tax"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TaxRepository is the Postgres implementation of tax.Repository.
// Save uses upsert-by-id so a future admin "edit rule" workflow can
// reuse the same call without a separate Update method.
type TaxRepository struct {
	pool *pgxpool.Pool
}

func NewTaxRepository(pool *pgxpool.Pool) *TaxRepository {
	return &TaxRepository{pool: pool}
}

var _ tax.Repository = (*TaxRepository)(nil)

const taxColumns = `id, name, kind, country, city, currency, rate_pct_bips, flat_amount_cents, max_nights, effective_from, effective_until`

func (r *TaxRepository) Save(ctx context.Context, rule *tax.Rule) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tax_rules (`+taxColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO UPDATE SET
			name              = EXCLUDED.name,
			kind              = EXCLUDED.kind,
			country           = EXCLUDED.country,
			city              = EXCLUDED.city,
			currency          = EXCLUDED.currency,
			rate_pct_bips     = EXCLUDED.rate_pct_bips,
			flat_amount_cents = EXCLUDED.flat_amount_cents,
			max_nights        = EXCLUDED.max_nights,
			effective_from    = EXCLUDED.effective_from,
			effective_until   = EXCLUDED.effective_until`,
		rule.ID, rule.Name, string(rule.Kind), rule.Country, rule.City, rule.Currency,
		rule.RatePctBips, rule.FlatAmountCents, rule.MaxNights, rule.EffectiveFrom, rule.EffectiveUntil,
	)
	return mapError(err)
}

func (r *TaxRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM tax_rules WHERE id = $1`, id)
	return mapError(err)
}

func (r *TaxRepository) List(ctx context.Context) ([]*tax.Rule, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+taxColumns+` FROM tax_rules ORDER BY lower(name)`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanTaxRules(rows)
}

// RulesFor pre-filters by country in SQL — the most-selective column
// without forcing the caller to know about case-folding. The matcher
// in Calculate does the case-insensitive city/window pass on the
// returned set; the SQL filter just trims the candidate list.
func (r *TaxRepository) RulesFor(ctx context.Context, country, city string, at time.Time) ([]*tax.Rule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+taxColumns+`
		  FROM tax_rules
		 WHERE (country = '' OR upper(country) = upper($1))`,
		strings.TrimSpace(country),
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	all, err := scanTaxRules(rows)
	if err != nil {
		return nil, err
	}
	out := make([]*tax.Rule, 0, len(all))
	for _, ru := range all {
		if ru.Matches(country, city, at) {
			out = append(out, ru)
		}
	}
	return out, nil
}

// scanTaxRules consumes a tax-row query result into the domain type.
// Kept private; both List and RulesFor share the same column order.
func scanTaxRules(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*tax.Rule, error) {
	out := make([]*tax.Rule, 0)
	for rows.Next() {
		var (
			ru       tax.Rule
			kindStr  string
		)
		if err := rows.Scan(&ru.ID, &ru.Name, &kindStr, &ru.Country, &ru.City, &ru.Currency,
			&ru.RatePctBips, &ru.FlatAmountCents, &ru.MaxNights, &ru.EffectiveFrom, &ru.EffectiveUntil); err != nil {
			return nil, mapError(err)
		}
		ru.Kind = tax.Kind(kindStr)
		out = append(out, &ru)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return out, nil
}
