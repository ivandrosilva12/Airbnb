package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/airhost/backend/internal/domain/dispute"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DisputeRepository is the Postgres implementation of dispute.Repository.
// Save is a full UPSERT of the aggregate (dispute row + evidence child rows);
// the partial-unique index in migration 0040 enforces "one active dispute per
// booking" at the database level.
type DisputeRepository struct {
	pool *pgxpool.Pool
}

// NewDisputeRepository builds a DisputeRepository.
func NewDisputeRepository(pool *pgxpool.Pool) *DisputeRepository {
	return &DisputeRepository{pool: pool}
}

const disputeColumns = `id, booking_id, opener_id, kind, reason, requested_amount_cents,
	currency, status, host_response, resolution, admin_id, opened_at, decided_at, updated_at`

func (r *DisputeRepository) Save(ctx context.Context, d *dispute.Dispute) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mapError(err)
	}
	defer tx.Rollback(ctx)

	adminID := nilUUID(d.AdminID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO disputes (`+disputeColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (id) DO UPDATE
			SET kind = EXCLUDED.kind,
			    reason = EXCLUDED.reason,
			    requested_amount_cents = EXCLUDED.requested_amount_cents,
			    currency = EXCLUDED.currency,
			    status = EXCLUDED.status,
			    host_response = EXCLUDED.host_response,
			    resolution = EXCLUDED.resolution,
			    admin_id = EXCLUDED.admin_id,
			    decided_at = EXCLUDED.decided_at,
			    updated_at = EXCLUDED.updated_at`,
		d.ID, d.BookingID, d.OpenerID, string(d.Kind), d.Reason, d.RequestedAmountCents,
		d.Currency, string(d.Status), d.HostResponse, d.Resolution, adminID,
		d.OpenedAt, d.DecidedAt, d.UpdatedAt,
	); err != nil {
		return mapError(err)
	}
	// Replace the evidence collection: simplest correct approach for a small
	// child table that grows append-only. The fk has ON DELETE CASCADE so the
	// child rows go with a dispute deletion.
	if _, err := tx.Exec(ctx, `DELETE FROM dispute_evidence WHERE dispute_id=$1`, d.ID); err != nil {
		return mapError(err)
	}
	for _, ev := range d.Evidence {
		if _, err := tx.Exec(ctx, `
			INSERT INTO dispute_evidence (id, dispute_id, url, note, added_by, added_at)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			ev.ID, d.ID, ev.URL, ev.Note, ev.AddedBy, ev.AddedAt,
		); err != nil {
			return mapError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return mapError(err)
	}
	return nil
}

func (r *DisputeRepository) FindByID(ctx context.Context, id uuid.UUID) (*dispute.Dispute, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+disputeColumns+` FROM disputes WHERE id=$1`, id)
	d, err := scanDispute(row)
	if err != nil {
		return nil, err
	}
	if err := r.loadEvidence(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (r *DisputeRepository) FindActiveByBooking(ctx context.Context, bookingID uuid.UUID) (*dispute.Dispute, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+disputeColumns+` FROM disputes
		WHERE booking_id=$1 AND status IN ('open','under_review')
		LIMIT 1`, bookingID)
	d, err := scanDispute(row)
	if err != nil {
		return nil, err
	}
	if err := r.loadEvidence(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (r *DisputeRepository) ListByOpener(ctx context.Context, openerID uuid.UUID, page shared.Page) (shared.PageResult[*dispute.Dispute], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM disputes WHERE opener_id=$1`, openerID,
	).Scan(&total); err != nil {
		return shared.PageResult[*dispute.Dispute]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+disputeColumns+` FROM disputes WHERE opener_id=$1 ORDER BY opened_at DESC, id DESC LIMIT $2 OFFSET $3`,
		openerID, page.Limit, page.Offset)
	if err != nil {
		return shared.PageResult[*dispute.Dispute]{}, mapError(err)
	}
	defer rows.Close()
	items, err := scanDisputeRows(rows)
	if err != nil {
		return shared.PageResult[*dispute.Dispute]{}, err
	}
	if err := r.loadEvidenceMany(ctx, items); err != nil {
		return shared.PageResult[*dispute.Dispute]{}, err
	}
	return shared.PageResult[*dispute.Dispute]{Items: items, Total: total}, nil
}

func (r *DisputeRepository) ListByBookings(ctx context.Context, bookingIDs []uuid.UUID, page shared.Page) (shared.PageResult[*dispute.Dispute], error) {
	if len(bookingIDs) == 0 {
		return shared.PageResult[*dispute.Dispute]{}, nil
	}
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM disputes WHERE booking_id = ANY($1)`, bookingIDs,
	).Scan(&total); err != nil {
		return shared.PageResult[*dispute.Dispute]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+disputeColumns+` FROM disputes WHERE booking_id = ANY($1) ORDER BY opened_at DESC, id DESC LIMIT $2 OFFSET $3`,
		bookingIDs, page.Limit, page.Offset)
	if err != nil {
		return shared.PageResult[*dispute.Dispute]{}, mapError(err)
	}
	defer rows.Close()
	items, err := scanDisputeRows(rows)
	if err != nil {
		return shared.PageResult[*dispute.Dispute]{}, err
	}
	if err := r.loadEvidenceMany(ctx, items); err != nil {
		return shared.PageResult[*dispute.Dispute]{}, err
	}
	return shared.PageResult[*dispute.Dispute]{Items: items, Total: total}, nil
}

func (r *DisputeRepository) ListOpen(ctx context.Context, page shared.Page) (shared.PageResult[*dispute.Dispute], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM disputes WHERE status IN ('open','under_review')`,
	).Scan(&total); err != nil {
		return shared.PageResult[*dispute.Dispute]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+disputeColumns+` FROM disputes WHERE status IN ('open','under_review')
		 ORDER BY opened_at ASC, id ASC LIMIT $1 OFFSET $2`,
		page.Limit, page.Offset)
	if err != nil {
		return shared.PageResult[*dispute.Dispute]{}, mapError(err)
	}
	defer rows.Close()
	items, err := scanDisputeRows(rows)
	if err != nil {
		return shared.PageResult[*dispute.Dispute]{}, err
	}
	if err := r.loadEvidenceMany(ctx, items); err != nil {
		return shared.PageResult[*dispute.Dispute]{}, err
	}
	return shared.PageResult[*dispute.Dispute]{Items: items, Total: total}, nil
}

func scanDispute(row pgx.Row) (*dispute.Dispute, error) {
	var (
		d         dispute.Dispute
		kind      string
		status    string
		adminID   *uuid.UUID
		decidedAt *time.Time
	)
	err := row.Scan(&d.ID, &d.BookingID, &d.OpenerID, &kind, &d.Reason, &d.RequestedAmountCents,
		&d.Currency, &status, &d.HostResponse, &d.Resolution, &adminID,
		&d.OpenedAt, &decidedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.ErrNotFound
		}
		return nil, mapError(err)
	}
	d.Kind = dispute.Kind(kind)
	d.Status = dispute.Status(status)
	if adminID != nil {
		d.AdminID = *adminID
	}
	d.DecidedAt = decidedAt
	return &d, nil
}

func scanDisputeRows(rows pgx.Rows) ([]*dispute.Dispute, error) {
	var out []*dispute.Dispute
	for rows.Next() {
		var (
			d         dispute.Dispute
			kind      string
			status    string
			adminID   *uuid.UUID
			decidedAt *time.Time
		)
		if err := rows.Scan(&d.ID, &d.BookingID, &d.OpenerID, &kind, &d.Reason, &d.RequestedAmountCents,
			&d.Currency, &status, &d.HostResponse, &d.Resolution, &adminID,
			&d.OpenedAt, &decidedAt, &d.UpdatedAt); err != nil {
			return nil, mapError(err)
		}
		d.Kind = dispute.Kind(kind)
		d.Status = dispute.Status(status)
		if adminID != nil {
			d.AdminID = *adminID
		}
		d.DecidedAt = decidedAt
		out = append(out, &d)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return out, nil
}

func (r *DisputeRepository) loadEvidence(ctx context.Context, d *dispute.Dispute) error {
	rows, err := r.pool.Query(ctx, `
		SELECT id, url, note, added_by, added_at FROM dispute_evidence
		WHERE dispute_id=$1 ORDER BY added_at ASC, id ASC`, d.ID)
	if err != nil {
		return mapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var ev dispute.Evidence
		if err := rows.Scan(&ev.ID, &ev.URL, &ev.Note, &ev.AddedBy, &ev.AddedAt); err != nil {
			return mapError(err)
		}
		d.Evidence = append(d.Evidence, ev)
	}
	return mapError(rows.Err())
}

func (r *DisputeRepository) loadEvidenceMany(ctx context.Context, items []*dispute.Dispute) error {
	for _, d := range items {
		if err := r.loadEvidence(ctx, d); err != nil {
			return err
		}
	}
	return nil
}
