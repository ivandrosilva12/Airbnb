package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/report"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReportRepository is the Postgres implementation of report.Repository.
type ReportRepository struct {
	pool *pgxpool.Pool
}

// NewReportRepository builds a ReportRepository.
func NewReportRepository(pool *pgxpool.Pool) *ReportRepository {
	return &ReportRepository{pool: pool}
}

const reportColumns = `id, property_id, reporter_id, reason, note, status,
	resolution, resolver_id, resolved_at, created_at, updated_at`

func (r *ReportRepository) Create(ctx context.Context, rep *report.Report) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO reports (`+reportColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		rep.ID, rep.PropertyID, rep.ReporterID, string(rep.Reason), rep.Note, string(rep.Status),
		rep.Resolution, nilUUID(rep.ResolverID), rep.ResolvedAt, rep.CreatedAt, rep.UpdatedAt,
	)
	return mapError(err)
}

func (r *ReportRepository) Update(ctx context.Context, rep *report.Report) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE reports
		SET status=$2, resolution=$3, resolver_id=$4, resolved_at=$5, updated_at=$6
		WHERE id=$1`,
		rep.ID, string(rep.Status), rep.Resolution, nilUUID(rep.ResolverID), rep.ResolvedAt, rep.UpdatedAt,
	)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *ReportRepository) FindByID(ctx context.Context, id uuid.UUID) (*report.Report, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+reportColumns+` FROM reports WHERE id=$1`, id)
	return scanReport(row)
}

func (r *ReportRepository) ListByStatus(ctx context.Context, status report.Status, page shared.Page) (shared.PageResult[*report.Report], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM reports WHERE status=$1`, string(status),
	).Scan(&total); err != nil {
		return shared.PageResult[*report.Report]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+reportColumns+` FROM reports WHERE status=$1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		string(status), page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*report.Report]{}, mapError(err)
	}
	defer rows.Close()

	var items []*report.Report
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			return shared.PageResult[*report.Report]{}, err
		}
		items = append(items, rep)
	}
	return shared.PageResult[*report.Report]{Items: items, Total: total}, mapError(rows.Err())
}

func (r *ReportRepository) HasOpenFromReporter(ctx context.Context, propertyID, reporterID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM reports
			WHERE property_id=$1 AND reporter_id=$2 AND status='open'
		)`, propertyID, reporterID,
	).Scan(&exists)
	return exists, mapError(err)
}

func scanReport(row rowScanner) (*report.Report, error) {
	var (
		rep        report.Report
		reason     string
		status     string
		resolverID *uuid.UUID
	)
	if err := row.Scan(&rep.ID, &rep.PropertyID, &rep.ReporterID, &reason, &rep.Note, &status,
		&rep.Resolution, &resolverID, &rep.ResolvedAt, &rep.CreatedAt, &rep.UpdatedAt); err != nil {
		return nil, mapError(err)
	}
	rep.Reason = report.Reason(reason)
	rep.Status = report.Status(status)
	if resolverID != nil {
		rep.ResolverID = *resolverID
	}
	return &rep, nil
}
