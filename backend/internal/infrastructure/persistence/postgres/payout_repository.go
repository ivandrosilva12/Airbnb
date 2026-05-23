package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PayoutRepository is the Postgres implementation of payout.Repository.
type PayoutRepository struct {
	pool *pgxpool.Pool
}

// NewPayoutRepository builds a PayoutRepository.
func NewPayoutRepository(pool *pgxpool.Pool) *PayoutRepository {
	return &PayoutRepository{pool: pool}
}

const payoutColumns = `id, host_id, booking_id, property_id, kind, amount_cents, currency, created_at`

func (r *PayoutRepository) Create(ctx context.Context, e *payout.Entry) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO payouts (`+payoutColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, e.HostID, e.BookingID, e.PropertyID, string(e.Kind),
		e.Amount.AmountCents(), e.Amount.Currency(), e.CreatedAt,
	)
	return mapError(err)
}

func (r *PayoutRepository) ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*payout.Entry], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM payouts WHERE host_id=$1`, hostID,
	).Scan(&total); err != nil {
		return shared.PageResult[*payout.Entry]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+payoutColumns+` FROM payouts WHERE host_id=$1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		hostID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*payout.Entry]{}, mapError(err)
	}
	defer rows.Close()

	var items []*payout.Entry
	for rows.Next() {
		e, err := scanPayout(rows)
		if err != nil {
			return shared.PageResult[*payout.Entry]{}, err
		}
		items = append(items, e)
	}
	return shared.PageResult[*payout.Entry]{Items: items, Total: total}, mapError(rows.Err())
}

func (r *PayoutRepository) HasEarningForBooking(ctx context.Context, bookingID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM payouts WHERE booking_id=$1 AND kind='earning')`, bookingID,
	).Scan(&exists)
	return exists, mapError(err)
}

func (r *PayoutRepository) BalancesByHost(ctx context.Context, hostID uuid.UUID) ([]payout.Balance, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT currency,
		       COALESCE(SUM(amount_cents) FILTER (WHERE kind='earning'), 0) AS earned,
		       COALESCE(SUM(amount_cents) FILTER (WHERE kind='refund'), 0)  AS refunded
		FROM payouts WHERE host_id=$1
		GROUP BY currency
		ORDER BY currency`, hostID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []payout.Balance
	for rows.Next() {
		var b payout.Balance
		if err := rows.Scan(&b.Currency, &b.EarnedCents, &b.RefundedCents); err != nil {
			return nil, mapError(err)
		}
		out = append(out, b)
	}
	return out, mapError(rows.Err())
}

func scanPayout(row rowScanner) (*payout.Entry, error) {
	var (
		e           payout.Entry
		kind        string
		amountCents int64
		currency    string
	)
	err := row.Scan(
		&e.ID, &e.HostID, &e.BookingID, &e.PropertyID, &kind, &amountCents, &currency, &e.CreatedAt,
	)
	if err != nil {
		return nil, mapError(err)
	}
	e.Kind = payout.Kind(kind)
	money, _ := shared.NewMoney(amountCents, currency)
	e.Amount = money
	return &e, nil
}
