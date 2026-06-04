package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/messagetemplate"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MessageTemplateRepository is the Postgres impl of messagetemplate.Repository.
type MessageTemplateRepository struct {
	pool *pgxpool.Pool
}

// NewMessageTemplateRepository builds a MessageTemplateRepository.
func NewMessageTemplateRepository(pool *pgxpool.Pool) *MessageTemplateRepository {
	return &MessageTemplateRepository{pool: pool}
}

var _ messagetemplate.Repository = (*MessageTemplateRepository)(nil)

const messageTemplateColumns = `id, host_id, label, body, created_at, updated_at`

func (r *MessageTemplateRepository) Create(ctx context.Context, t *messagetemplate.Template) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO message_templates (`+messageTemplateColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		t.ID, t.HostID, t.Label, t.Body, t.CreatedAt, t.UpdatedAt,
	)
	return mapError(err)
}

func (r *MessageTemplateRepository) Update(ctx context.Context, t *messagetemplate.Template) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE message_templates SET label=$2, body=$3, updated_at=$4 WHERE id=$1`,
		t.ID, t.Label, t.Body, t.UpdatedAt,
	)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *MessageTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM message_templates WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *MessageTemplateRepository) FindByID(ctx context.Context, id uuid.UUID) (*messagetemplate.Template, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+messageTemplateColumns+` FROM message_templates WHERE id=$1`, id)
	var t messagetemplate.Template
	if err := row.Scan(&t.ID, &t.HostID, &t.Label, &t.Body, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, mapError(err)
	}
	return &t, nil
}

// EraseByHost hard-deletes every template owned by the host — the GDPR
// right-to-erasure path. Templates are author-scoped preference data
// with no retention basis. Note: the FK already has ON DELETE CASCADE,
// so deleting the user row would do this implicitly — we still issue
// the DELETE explicitly so the erase orchestrator gets a row count and
// the audit metadata reflects what we touched.
func (r *MessageTemplateRepository) EraseByHost(ctx context.Context, hostID uuid.UUID) (int, error) {
	ct, err := r.pool.Exec(ctx, `DELETE FROM message_templates WHERE host_id=$1`, hostID)
	if err != nil {
		return 0, mapError(err)
	}
	return int(ct.RowsAffected()), nil
}

func (r *MessageTemplateRepository) ListByHost(ctx context.Context, hostID uuid.UUID) ([]*messagetemplate.Template, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+messageTemplateColumns+` FROM message_templates
		 WHERE host_id=$1 ORDER BY lower(label)`,
		hostID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	out := make([]*messagetemplate.Template, 0)
	for rows.Next() {
		var t messagetemplate.Template
		if err := rows.Scan(&t.ID, &t.HostID, &t.Label, &t.Body, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, mapError(err)
		}
		out = append(out, &t)
	}
	return out, mapError(rows.Err())
}
