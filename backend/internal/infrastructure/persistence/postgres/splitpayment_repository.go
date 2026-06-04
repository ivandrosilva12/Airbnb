package postgres

import (
	"context"
	"strings"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SplitPaymentRepository is the Postgres implementation of splitpayment.Repository.
// Aggregate writes (Create/Update) span two tables (split_payments + split_payment_shares)
// and must commit atomically. When the repository is bound to a *pgxpool.Pool
// it manages its own inner transaction; when bound to a pgx.Tx (i.e. running
// inside a UnitOfWork — S31) it reuses the outer transaction so the split write
// and the outbox event commit together.
type SplitPaymentRepository struct {
	db querier
}

// NewSplitPaymentRepository builds a pool-bound SplitPaymentRepository. Direct
// callers (outside a UnitOfWork) get the inner-transaction behaviour for free.
func NewSplitPaymentRepository(pool *pgxpool.Pool) *SplitPaymentRepository {
	return &SplitPaymentRepository{db: pool}
}

// NewSplitPaymentTxRepository binds the repository to an existing transaction.
// Use this from a UnitOfWork so the parent + shares write joins the outer tx
// instead of opening a nested savepoint.
func NewSplitPaymentTxRepository(tx pgx.Tx) *SplitPaymentRepository {
	return &SplitPaymentRepository{db: tx}
}

// txBeginner is the subset of *pgxpool.Pool needed to open an inner tx for
// compound writes. pgx.Tx does NOT satisfy it (its Begin returns a savepoint,
// which we deliberately avoid by skipping Begin altogether when already in a tx).
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

var _ splitpayment.Repository = (*SplitPaymentRepository)(nil)

const splitPaymentColumns = `id, booking_id, organizer_id, currency, total_cents, status,
		created_at, updated_at, completed_at, cancelled_at`

const splitShareColumns = `id, split_payment_id, payer_email, payer_user_id, amount_cents,
		status, gateway_ref, failure_reason, paid_at, created_at, updated_at`

func (r *SplitPaymentRepository) Create(ctx context.Context, sp *splitpayment.SplitPayment) error {
	// When bound to a Pool, open an inner tx so the parent + shares are
	// atomic. When already inside an outer tx (UoW path), reuse it.
	if pool, ok := r.db.(txBeginner); ok {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return mapError(err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck // commit overrides on success
		if err := createSplitWithin(ctx, tx, sp); err != nil {
			return err
		}
		return mapError(tx.Commit(ctx))
	}
	return createSplitWithin(ctx, r.db, sp)
}

func createSplitWithin(ctx context.Context, q querier, sp *splitpayment.SplitPayment) error {
	if _, err := q.Exec(ctx, `
		INSERT INTO split_payments (`+splitPaymentColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		sp.ID, sp.BookingID, sp.OrganizerID, sp.Currency, sp.TotalCents,
		string(sp.Status), sp.CreatedAt, sp.UpdatedAt, sp.CompletedAt, sp.CancelledAt,
	); err != nil {
		return mapError(err)
	}
	return insertShares(ctx, q, sp.Shares)
}

func (r *SplitPaymentRepository) Update(ctx context.Context, sp *splitpayment.SplitPayment) error {
	if pool, ok := r.db.(txBeginner); ok {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return mapError(err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		if err := updateSplitWithin(ctx, tx, sp); err != nil {
			return err
		}
		return mapError(tx.Commit(ctx))
	}
	return updateSplitWithin(ctx, r.db, sp)
}

func updateSplitWithin(ctx context.Context, q querier, sp *splitpayment.SplitPayment) error {
	ct, err := q.Exec(ctx, `
		UPDATE split_payments
		   SET status=$2, updated_at=$3, completed_at=$4, cancelled_at=$5
		 WHERE id=$1`,
		sp.ID, string(sp.Status), sp.UpdatedAt, sp.CompletedAt, sp.CancelledAt,
	)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	// Refresh shares with a delete-then-insert so the parent and shares stay
	// in lockstep. The set is tiny (typically 2-5 shares), so the cost is
	// negligible and the logic is easier to reason about than per-share
	// diffing.
	if _, err := q.Exec(ctx, `DELETE FROM split_payment_shares WHERE split_payment_id=$1`, sp.ID); err != nil {
		return mapError(err)
	}
	return insertShares(ctx, q, sp.Shares)
}

func insertShares(ctx context.Context, q querier, shares []splitpayment.Share) error {
	for _, sh := range shares {
		if _, err := q.Exec(ctx, `
			INSERT INTO split_payment_shares (`+splitShareColumns+`)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			sh.ID, sh.SplitPaymentID, sh.PayerEmail, sh.PayerUserID, sh.AmountCents,
			string(sh.Status), sh.GatewayRef, sh.FailureReason, sh.PaidAt, sh.CreatedAt, sh.UpdatedAt,
		); err != nil {
			return mapError(err)
		}
	}
	return nil
}

func (r *SplitPaymentRepository) FindByID(ctx context.Context, id uuid.UUID) (*splitpayment.SplitPayment, error) {
	return r.loadOne(ctx, `SELECT `+splitPaymentColumns+` FROM split_payments WHERE id=$1`, id)
}

func (r *SplitPaymentRepository) FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*splitpayment.SplitPayment, error) {
	return r.loadOne(ctx, `SELECT `+splitPaymentColumns+` FROM split_payments WHERE booking_id=$1`, bookingID)
}

func (r *SplitPaymentRepository) loadOne(ctx context.Context, query string, arg uuid.UUID) (*splitpayment.SplitPayment, error) {
	row := r.db.QueryRow(ctx, query, arg)
	sp, err := scanSplitPayment(row)
	if err != nil {
		return nil, err
	}
	shares, err := r.loadShares(ctx, sp.ID)
	if err != nil {
		return nil, err
	}
	sp.Shares = shares
	return sp, nil
}

func (r *SplitPaymentRepository) loadShares(ctx context.Context, splitID uuid.UUID) ([]splitpayment.Share, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+splitShareColumns+` FROM split_payment_shares WHERE split_payment_id=$1 ORDER BY created_at`,
		splitID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	out := make([]splitpayment.Share, 0)
	for rows.Next() {
		var sh splitpayment.Share
		var status string
		if err := rows.Scan(
			&sh.ID, &sh.SplitPaymentID, &sh.PayerEmail, &sh.PayerUserID, &sh.AmountCents,
			&status, &sh.GatewayRef, &sh.FailureReason, &sh.PaidAt, &sh.CreatedAt, &sh.UpdatedAt,
		); err != nil {
			return nil, mapError(err)
		}
		sh.Status = splitpayment.ShareStatus(status)
		out = append(out, sh)
	}
	return out, mapError(rows.Err())
}

func (r *SplitPaymentRepository) ListForUser(ctx context.Context, userID uuid.UUID, email string) ([]*splitpayment.SplitPayment, error) {
	emailLower := strings.ToLower(strings.TrimSpace(email))
	// Two-step lookup: ids the user has any stake in, then hydrate each
	// aggregate with its shares. Splits-per-user is small, so a per-id
	// fan-out is fine and keeps the SQL simple.
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT sp.id, sp.created_at FROM split_payments sp
		LEFT JOIN split_payment_shares sh ON sh.split_payment_id = sp.id
		WHERE sp.organizer_id = $1
		   OR ($2 <> '' AND lower(sh.payer_email) = $2)
		ORDER BY sp.created_at DESC`,
		userID, emailLower,
	)
	if err != nil {
		return nil, mapError(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var dummy interface{}
		if err := rows.Scan(&id, &dummy); err != nil {
			rows.Close()
			return nil, mapError(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	out := make([]*splitpayment.SplitPayment, 0, len(ids))
	for _, id := range ids {
		sp, err := r.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, nil
}

func scanSplitPayment(row interface{ Scan(...any) error }) (*splitpayment.SplitPayment, error) {
	var sp splitpayment.SplitPayment
	var status string
	if err := row.Scan(
		&sp.ID, &sp.BookingID, &sp.OrganizerID, &sp.Currency, &sp.TotalCents,
		&status, &sp.CreatedAt, &sp.UpdatedAt, &sp.CompletedAt, &sp.CancelledAt,
	); err != nil {
		return nil, mapError(err)
	}
	sp.Status = splitpayment.Status(status)
	return &sp, nil
}
