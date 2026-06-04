package postgres

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/savedsearch"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SavedSearchRepository is the Postgres implementation of savedsearch.Repository.
type SavedSearchRepository struct {
	pool *pgxpool.Pool
}

// NewSavedSearchRepository builds a SavedSearchRepository.
func NewSavedSearchRepository(pool *pgxpool.Pool) *SavedSearchRepository {
	return &SavedSearchRepository{pool: pool}
}

var _ savedsearch.Repository = (*SavedSearchRepository)(nil)

const savedSearchColumns = `id, user_id, name, query, alerts_enabled, last_notified_at, created_at`

func (r *SavedSearchRepository) Create(ctx context.Context, s *savedsearch.SavedSearch) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO saved_searches (`+savedSearchColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		s.ID, s.UserID, s.Name, s.Query, s.AlertsEnabled, s.LastNotifiedAt, s.CreatedAt,
	)
	return mapError(err)
}

func (r *SavedSearchRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]*savedsearch.SavedSearch, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+savedSearchColumns+` FROM saved_searches WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanSavedSearches(rows)
}

func (r *SavedSearchRepository) Delete(ctx context.Context, userID, id uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM saved_searches WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *SavedSearchRepository) SetAlerts(ctx context.Context, userID, id uuid.UUID, enabled bool) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE saved_searches SET alerts_enabled=$3 WHERE id=$1 AND user_id=$2`, id, userID, enabled)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *SavedSearchRepository) ListAlertable(ctx context.Context) ([]*savedsearch.SavedSearch, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+savedSearchColumns+` FROM saved_searches WHERE alerts_enabled=true`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanSavedSearches(rows)
}

func (r *SavedSearchRepository) MarkNotified(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE saved_searches SET last_notified_at=$2 WHERE id=$1`, id, at)
	return mapError(err)
}

// EraseByUser hard-deletes every saved search owned by the user — the
// GDPR right-to-erasure path. Pure preference data; no retention basis.
func (r *SavedSearchRepository) EraseByUser(ctx context.Context, userID uuid.UUID) (int, error) {
	ct, err := r.pool.Exec(ctx, `DELETE FROM saved_searches WHERE user_id=$1`, userID)
	if err != nil {
		return 0, mapError(err)
	}
	return int(ct.RowsAffected()), nil
}

func scanSavedSearches(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*savedsearch.SavedSearch, error) {
	var out []*savedsearch.SavedSearch
	for rows.Next() {
		var s savedsearch.SavedSearch
		if err := rows.Scan(&s.ID, &s.UserID, &s.Name, &s.Query, &s.AlertsEnabled, &s.LastNotifiedAt, &s.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		out = append(out, &s)
	}
	return out, mapError(rows.Err())
}
