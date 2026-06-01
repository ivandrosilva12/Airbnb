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
// Aggregate writes (Create/Update) are transactional: the parent row and
// every share row commit together so a partially-persisted split cannot
// linger after a process crash.
type SplitPaymentRepository struct {
	pool *pgxpool.Pool
}

// NewSplitPaymentRepository builds a SplitPaymentRepository.
func NewSplitPaymentRepository(pool *pgxpool.Pool) *SplitPaymentRepository {
	return &SplitPaymentRepository{pool: pool}
}

var _ splitpayment.Repository = (*SplitPaymentRepository)(nil)

const splitPaymentColumns = `id, booking_id, organizer_id, currency, total_cents, status,
		created_at, updated_at, completed_at, cancelled_at`

const splitShareColumns = `id, split_payment_id, payer_email, payer_user_id, amount_cents,
		status, paid_at, created_at, updated_at`

func (r *SplitPaymentRepository) Create(ctx context.Context, sp *splitpayment.SplitPayment) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // commit overrides on success
	if _, err := tx.Exec(ctx, `
		INSERT INTO split_payments (`+splitPaymentColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		sp.ID, sp.BookingID, sp.OrganizerID, sp.Currency, sp.TotalCents,
		string(sp.Status), sp.CreatedAt, sp.UpdatedAt, sp.CompletedAt, sp.CancelledAt,
	); err != nil {
		return mapError(err)
	}
	if err := insertShares(ctx, tx, sp.Shares); err != nil {
		return err
	}
	return mapError(tx.Commit(ctx))
}

func (r *SplitPaymentRepository) Update(ctx context.Context, sp *splitpayment.SplitPayment) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	ct, err := tx.Exec(ctx, `
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
	if _, err := tx.Exec(ctx, `DELETE FROM split_payment_shares WHERE split_payment_id=$1`, sp.ID); err != nil {
		return mapError(err)
	}
	if err := insertShares(ctx, tx, sp.Shares); err != nil {
		return err
	}
	return mapError(tx.Commit(ctx))
}

func insertShares(ctx context.Context, tx pgx.Tx, shares []splitpayment.Share) error {
	for _, sh := range shares {
		if _, err := tx.Exec(ctx, `
			INSERT INTO split_payment_shares (`+splitShareColumns+`)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			sh.ID, sh.SplitPaymentID, sh.PayerEmail, sh.PayerUserID, sh.AmountCents,
			string(sh.Status), sh.PaidAt, sh.CreatedAt, sh.UpdatedAt,
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
	row := r.pool.QueryRow(ctx, query, arg)
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
	rows, err := r.pool.Query(ctx,
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
			&status, &sh.PaidAt, &sh.CreatedAt, &sh.UpdatedAt,
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
	rows, err := r.pool.Query(ctx, `
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
