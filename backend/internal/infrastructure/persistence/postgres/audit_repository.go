package postgres

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditRepository is the Postgres implementation of audit.Repository.
// Append-only by contract — there is no Update or Delete method on the
// port, and nothing in this file writes outside Create.
type AuditRepository struct {
	pool *pgxpool.Pool
}

func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

var _ audit.Repository = (*AuditRepository)(nil)

const auditColumns = `id, actor_id, action, target_type, target_id, metadata, request_id, created_at`

func (r *AuditRepository) Create(ctx context.Context, e *audit.Event) error {
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		// Metadata that won't JSON-marshal is a programmer bug, not a
		// runtime condition we ever expect; treat as validation.
		return shared.NewValidationError("audit: metadata not json-encodable: " + err.Error())
	}
	if e.Metadata == nil {
		meta = []byte("{}")
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO audit_events (`+auditColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, e.ActorID, string(e.Action), string(e.TargetType), e.TargetID, meta, e.RequestID, e.CreatedAt,
	)
	return mapError(err)
}

func (r *AuditRepository) List(ctx context.Context, f audit.Filter, page shared.Page) (shared.PageResult[*audit.Event], error) {
	// Build WHERE clause dynamically — the filter fields are all
	// optional so we'd rather assemble than carry NULL-aware predicates
	// in every query. Empty filter → full unfiltered scan, paginated.
	var where []string
	var args []any
	add := func(predicate string, v any) {
		args = append(args, v)
		where = append(where, predicate+"$"+itoa(len(args)))
	}
	if f.ActorID != uuid.Nil {
		add("actor_id = ", f.ActorID)
	}
	if f.Action != "" {
		add("action = ", string(f.Action))
	}
	if f.TargetType != "" {
		add("target_type = ", string(f.TargetType))
	}
	if f.TargetID != uuid.Nil {
		add("target_id = ", f.TargetID)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events`+clause, args...).Scan(&total); err != nil {
		return shared.PageResult[*audit.Event]{}, mapError(err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+auditColumns+` FROM audit_events`+clause+
			` ORDER BY created_at DESC, id DESC LIMIT $`+itoa(len(args)+1)+
			` OFFSET $`+itoa(len(args)+2),
		append(args, page.Limit, page.Offset)...,
	)
	if err != nil {
		return shared.PageResult[*audit.Event]{}, mapError(err)
	}
	defer rows.Close()

	out := make([]*audit.Event, 0)
	for rows.Next() {
		var (
			ev      audit.Event
			action  string
			tgt     string
			meta    []byte
			reqID   string
		)
		if err := rows.Scan(&ev.ID, &ev.ActorID, &action, &tgt, &ev.TargetID, &meta, &reqID, &ev.CreatedAt); err != nil {
			return shared.PageResult[*audit.Event]{}, mapError(err)
		}
		ev.Action = audit.Action(action)
		ev.TargetType = audit.TargetType(tgt)
		ev.RequestID = reqID
		if len(meta) > 0 {
			if err := json.Unmarshal(meta, &ev.Metadata); err != nil {
				return shared.PageResult[*audit.Event]{}, mapError(err)
			}
		}
		out = append(out, &ev)
	}
	if err := rows.Err(); err != nil {
		return shared.PageResult[*audit.Event]{}, mapError(err)
	}
	return shared.PageResult[*audit.Event]{Items: out, Total: total}, nil
}

// itoa is a stdlib-free strconv.Itoa equivalent for small positive
// numbers, used to build the parameter placeholders ($1, $2, …) at
// query-build time. Saves importing strconv for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
