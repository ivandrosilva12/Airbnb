package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PaymentRepository is the Postgres implementation of payment.Repository.
type PaymentRepository struct {
	pool *pgxpool.Pool
}

// NewPaymentRepository builds a PaymentRepository.
func NewPaymentRepository(pool *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{pool: pool}
}

const paymentColumns = `id, booking_id, guest_id, amount_cents, currency, status,
	gateway_ref, failure_reason, refunded_cents, created_at, updated_at`

func (r *PaymentRepository) Create(ctx context.Context, p *payment.Payment) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO payments (`+paymentColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		p.ID, p.BookingID, p.GuestID, p.Amount.AmountCents(), p.Amount.Currency(), string(p.Status),
		p.GatewayRef, p.FailureReason, p.RefundedCents, p.CreatedAt, p.UpdatedAt,
	)
	return mapError(err)
}

func (r *PaymentRepository) Update(ctx context.Context, p *payment.Payment) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE payments SET status=$2, gateway_ref=$3, failure_reason=$4, refunded_cents=$5, updated_at=$6
		WHERE id=$1`,
		p.ID, string(p.Status), p.GatewayRef, p.FailureReason, p.RefundedCents, p.UpdatedAt,
	)
	return mapError(err)
}

func (r *PaymentRepository) FindByID(ctx context.Context, id uuid.UUID) (*payment.Payment, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE id=$1`, id)
	return scanPayment(row)
}

func (r *PaymentRepository) FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*payment.Payment, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE booking_id=$1`, bookingID)
	return scanPayment(row)
}

func (r *PaymentRepository) FindByGatewayRef(ctx context.Context, gatewayRef string) (*payment.Payment, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE gateway_ref=$1`, gatewayRef)
	return scanPayment(row)
}

func (r *PaymentRepository) ListByGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*payment.Payment], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM payments WHERE guest_id=$1`, guestID,
	).Scan(&total); err != nil {
		return shared.PageResult[*payment.Payment]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+paymentColumns+` FROM payments WHERE guest_id=$1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		guestID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*payment.Payment]{}, mapError(err)
	}
	defer rows.Close()

	var items []*payment.Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return shared.PageResult[*payment.Payment]{}, err
		}
		items = append(items, p)
	}
	return shared.PageResult[*payment.Payment]{Items: items, Total: total}, mapError(rows.Err())
}

func (r *PaymentRepository) RevenueForBookings(ctx context.Context, bookingIDs []uuid.UUID) (payment.Revenue, error) {
	var rev payment.Revenue
	if len(bookingIDs) == 0 {
		return rev, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT status, currency, SUM(amount_cents)
		FROM payments WHERE booking_id = ANY($1)
		GROUP BY status, currency`, bookingIDs)
	if err != nil {
		return rev, mapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			status   string
			currency string
			cents    int64
		)
		if err := rows.Scan(&status, &currency, &cents); err != nil {
			return payment.Revenue{}, mapError(err)
		}
		rev.Currency = currency
		switch payment.Status(status) {
		case payment.StatusCaptured:
			rev.CapturedCents += cents
		case payment.StatusAuthorized:
			rev.PendingCents += cents
		}
	}
	return rev, mapError(rows.Err())
}

func scanPayment(row rowScanner) (*payment.Payment, error) {
	var (
		p           payment.Payment
		amountCents int64
		currency    string
		status      string
	)
	err := row.Scan(
		&p.ID, &p.BookingID, &p.GuestID, &amountCents, &currency, &status,
		&p.GatewayRef, &p.FailureReason, &p.RefundedCents, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, mapError(err)
	}
	p.Status = payment.Status(status)
	money, _ := shared.NewMoney(amountCents, currency)
	p.Amount = money
	return &p, nil
}
