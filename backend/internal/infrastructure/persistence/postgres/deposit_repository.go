package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DepositRepository is the Postgres implementation of payment.DepositRepository.
type DepositRepository struct {
	pool *pgxpool.Pool
}

// NewDepositRepository builds a DepositRepository.
func NewDepositRepository(pool *pgxpool.Pool) *DepositRepository {
	return &DepositRepository{pool: pool}
}

const depositColumns = `id, booking_id, guest_id, amount_cents, currency, captured_cents,
	status, gateway_ref, failure_reason, released_at, created_at, updated_at`

func (r *DepositRepository) Create(ctx context.Context, d *payment.DepositHold) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		INSERT INTO deposit_holds (`+depositColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		d.ID, d.BookingID, d.GuestID, d.Amount.AmountCents(), d.Amount.Currency(),
		d.CapturedCents, string(d.Status), d.GatewayRef, d.FailureReason,
		d.ReleasedAt, d.CreatedAt, d.UpdatedAt,
	); err != nil {
		return mapError(err)
	}
	if err := upsertDepositAdjustments(ctx, tx, d); err != nil {
		return mapError(err)
	}
	return mapError(tx.Commit(ctx))
}

func (r *DepositRepository) Update(ctx context.Context, d *payment.DepositHold) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		UPDATE deposit_holds SET captured_cents=$2, status=$3, gateway_ref=$4,
		    failure_reason=$5, released_at=$6, updated_at=$7
		WHERE id=$1`,
		d.ID, d.CapturedCents, string(d.Status), d.GatewayRef,
		d.FailureReason, d.ReleasedAt, d.UpdatedAt,
	); err != nil {
		return mapError(err)
	}
	if err := upsertDepositAdjustments(ctx, tx, d); err != nil {
		return mapError(err)
	}
	return mapError(tx.Commit(ctx))
}

// upsertDepositAdjustments inserts any new Adjustment rows the aggregate
// carries, ignoring duplicates so a replayed capture is a no-op.
func upsertDepositAdjustments(ctx context.Context, tx pgx.Tx, d *payment.DepositHold) error {
	for _, a := range d.Adjustments {
		var refID any
		if a.RefID != uuid.Nil {
			refID = a.RefID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO payment_adjustments
			    (id, payment_id, kind, amount_cents, reason, ref_kind, ref_id, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (id) DO NOTHING`,
			a.ID, d.ID, string(a.Kind), a.AmountCents, a.Reason, a.RefKind, refID, a.CreatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *DepositRepository) FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*payment.DepositHold, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+depositColumns+` FROM deposit_holds WHERE booking_id=$1`, bookingID)
	d, err := scanDeposit(row)
	if err != nil {
		return nil, err
	}
	if err := r.loadDepositAdjustments(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (r *DepositRepository) FindByID(ctx context.Context, id uuid.UUID) (*payment.DepositHold, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+depositColumns+` FROM deposit_holds WHERE id=$1`, id)
	d, err := scanDeposit(row)
	if err != nil {
		return nil, err
	}
	if err := r.loadDepositAdjustments(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (r *DepositRepository) loadDepositAdjustments(ctx context.Context, d *payment.DepositHold) error {
	rows, err := r.pool.Query(ctx, `
		SELECT id, payment_id, kind, amount_cents, reason, ref_kind, ref_id, created_at
		FROM payment_adjustments WHERE payment_id=$1 AND kind='deposit_capture'
		ORDER BY created_at ASC, id ASC`, d.ID)
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
	d.Adjustments = adjustments
	return nil
}

func scanDeposit(row rowScanner) (*payment.DepositHold, error) {
	var (
		d           payment.DepositHold
		amountCents int64
		currency    string
		status      string
	)
	err := row.Scan(
		&d.ID, &d.BookingID, &d.GuestID, &amountCents, &currency,
		&d.CapturedCents, &status, &d.GatewayRef, &d.FailureReason,
		&d.ReleasedAt, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, mapError(err)
	}
	d.Status = payment.DepositStatus(status)
	money, _ := shared.NewMoney(amountCents, currency)
	d.Amount = money
	return &d, nil
}
