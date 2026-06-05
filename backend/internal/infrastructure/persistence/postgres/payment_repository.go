package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PaymentRepository is the Postgres implementation of payment.Repository.
//
// The repository can run against either a *pgxpool.Pool (own connection,
// auto-managed transaction for Create/Update) or a pgx.Tx (a UnitOfWork's
// transaction, so the payment write commits atomically with the outbox
// append). Use NewPaymentRepository for the pool flavour and
// NewPaymentTxRepository inside a UnitOfWork (S123).
type PaymentRepository struct {
	pool querier
	// tx is non-nil when the repository is bound to an active transaction.
	// Create/Update then run their statements directly against tx instead
	// of starting a fresh inner transaction. Read methods always run
	// through pool (which is the same tx when bound).
	tx pgx.Tx
}

// NewPaymentRepository builds a PaymentRepository that owns its connections.
// Create/Update each open an inner transaction so the payment row and its
// payment_adjustments rows commit together.
func NewPaymentRepository(pool *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{pool: pool}
}

// NewPaymentTxRepository binds the repository to an active pgx.Tx so its
// writes participate in the caller's UnitOfWork — the payment row + any
// adjustment rows commit atomically with the outbox append the UoW
// performs (S123 — Payment UoW slice). Reads also run on the same
// transaction so a Create followed by a Find inside the same UoW sees the
// uncommitted row.
func NewPaymentTxRepository(tx pgx.Tx) *PaymentRepository {
	return &PaymentRepository{pool: tx, tx: tx}
}

const paymentColumns = `id, booking_id, guest_id, amount_cents, currency, status,
	gateway_ref, failure_reason, refunded_cents, damage_claim_cents, created_at, updated_at`

func (r *PaymentRepository) Create(ctx context.Context, p *payment.Payment) error {
	// When bound to an outer UnitOfWork transaction, write directly against
	// it so a downstream outbox failure rolls back the payment row too.
	// When running standalone, wrap the two-statement INSERT (payment +
	// adjustments) in a private tx so a crash mid-flow doesn't leave a
	// payment row without its adjustments.
	if r.tx != nil {
		return r.createWithin(ctx, r.tx, p)
	}
	pool, ok := r.pool.(*pgxpool.Pool)
	if !ok {
		// Should never happen — the constructor always installs a pool or
		// a tx — but if it does we degrade to running statements without
		// an inner transaction rather than panic.
		return r.createWithin(ctx, r.pool, p)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := r.createWithin(ctx, tx, p); err != nil {
		return err
	}
	return mapError(tx.Commit(ctx))
}

func (r *PaymentRepository) createWithin(ctx context.Context, q querier, p *payment.Payment) error {
	if _, err := q.Exec(ctx, `
		INSERT INTO payments (`+paymentColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		p.ID, p.BookingID, p.GuestID, p.Amount.AmountCents(), p.Amount.Currency(), string(p.Status),
		p.GatewayRef, p.FailureReason, p.RefundedCents, p.DamageClaimCents, p.CreatedAt, p.UpdatedAt,
	); err != nil {
		return mapError(err)
	}
	if err := upsertAdjustments(ctx, q, p); err != nil {
		return mapError(err)
	}
	return nil
}

func (r *PaymentRepository) Update(ctx context.Context, p *payment.Payment) error {
	if r.tx != nil {
		return r.updateWithin(ctx, r.tx, p)
	}
	pool, ok := r.pool.(*pgxpool.Pool)
	if !ok {
		return r.updateWithin(ctx, r.pool, p)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := r.updateWithin(ctx, tx, p); err != nil {
		return err
	}
	return mapError(tx.Commit(ctx))
}

func (r *PaymentRepository) updateWithin(ctx context.Context, q querier, p *payment.Payment) error {
	if _, err := q.Exec(ctx, `
		UPDATE payments SET status=$2, gateway_ref=$3, failure_reason=$4,
		    refunded_cents=$5, damage_claim_cents=$6, updated_at=$7
		WHERE id=$1`,
		p.ID, string(p.Status), p.GatewayRef, p.FailureReason,
		p.RefundedCents, p.DamageClaimCents, p.UpdatedAt,
	); err != nil {
		return mapError(err)
	}
	if err := upsertAdjustments(ctx, q, p); err != nil {
		return mapError(err)
	}
	return nil
}

// upsertAdjustments inserts any Adjustment rows the aggregate carries that
// have not been persisted yet. ON CONFLICT DO NOTHING makes re-applying the
// same dispute outcome a no-op (idempotency at storage level), complementing
// the aggregate's HasAdjustmentFor guard.
func upsertAdjustments(ctx context.Context, q querier, p *payment.Payment) error {
	for _, a := range p.Adjustments {
		var refID any
		if a.RefID != uuid.Nil {
			refID = a.RefID
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO payment_adjustments
			    (id, payment_id, kind, amount_cents, reason, ref_kind, ref_id, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (id) DO NOTHING`,
			a.ID, p.ID, string(a.Kind), a.AmountCents, a.Reason, a.RefKind, refID, a.CreatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *PaymentRepository) FindByID(ctx context.Context, id uuid.UUID) (*payment.Payment, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE id=$1`, id)
	p, err := scanPayment(row)
	if err != nil {
		return nil, err
	}
	if err := r.loadAdjustments(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (r *PaymentRepository) FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*payment.Payment, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE booking_id=$1`, bookingID)
	p, err := scanPayment(row)
	if err != nil {
		return nil, err
	}
	if err := r.loadAdjustments(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (r *PaymentRepository) FindByGatewayRef(ctx context.Context, gatewayRef string) (*payment.Payment, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE gateway_ref=$1`, gatewayRef)
	p, err := scanPayment(row)
	if err != nil {
		return nil, err
	}
	if err := r.loadAdjustments(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (r *PaymentRepository) loadAdjustments(ctx context.Context, p *payment.Payment) error {
	rows, err := r.pool.Query(ctx, `
		SELECT id, payment_id, kind, amount_cents, reason, ref_kind, ref_id, created_at
		FROM payment_adjustments WHERE payment_id=$1 ORDER BY created_at ASC, id ASC`, p.ID)
	if err != nil {
		return mapError(err)
	}
	defer rows.Close()
	var adjustments []payment.Adjustment
	for rows.Next() {
		var (
			a    payment.Adjustment
			kind string
			ref  *uuid.UUID
		)
		if err := rows.Scan(&a.ID, &a.PaymentID, &kind, &a.AmountCents, &a.Reason, &a.RefKind, &ref, &a.CreatedAt); err != nil {
			return mapError(err)
		}
		a.Kind = payment.AdjustmentKind(kind)
		if ref != nil {
			a.RefID = *ref
		}
		adjustments = append(adjustments, a)
	}
	if err := rows.Err(); err != nil {
		return mapError(err)
	}
	p.Adjustments = adjustments
	return nil
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
		ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
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
	if err := rows.Err(); err != nil {
		return shared.PageResult[*payment.Payment]{}, mapError(err)
	}
	// Hydrate adjustments per item (small page; N round-trips is acceptable).
	for _, p := range items {
		if err := r.loadAdjustments(ctx, p); err != nil {
			return shared.PageResult[*payment.Payment]{}, err
		}
	}
	return shared.PageResult[*payment.Payment]{Items: items, Total: total}, nil
}

func (r *PaymentRepository) RevenueForBookings(ctx context.Context, bookingIDs []uuid.UUID) (payment.Revenue, error) {
	if len(bookingIDs) == 0 {
		return payment.Revenue{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT status, currency, SUM(amount_cents), SUM(refunded_cents)
		FROM payments WHERE booking_id = ANY($1)
		GROUP BY status, currency`, bookingIDs)
	if err != nil {
		return payment.Revenue{}, mapError(err)
	}
	defer rows.Close()
	acc := payment.NewRevenueAccumulator()
	for rows.Next() {
		var (
			status   string
			currency string
			cents    int64
			refunded int64
		)
		if err := rows.Scan(&status, &currency, &cents, &refunded); err != nil {
			return payment.Revenue{}, mapError(err)
		}
		acc.Add(currency, payment.Status(status), cents, refunded)
	}
	if err := rows.Err(); err != nil {
		return payment.Revenue{}, mapError(err)
	}
	return acc.Dominant(), nil
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
		&p.GatewayRef, &p.FailureReason, &p.RefundedCents, &p.DamageClaimCents, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, mapError(err)
	}
	p.Status = payment.Status(status)
	money, _ := shared.NewMoney(amountCents, currency)
	p.Amount = money
	return &p, nil
}
