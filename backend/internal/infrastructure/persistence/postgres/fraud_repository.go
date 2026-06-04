package postgres

import (
	"context"
	"encoding/json"

	"github.com/airhost/backend/internal/domain/fraud"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FraudRepository is the Postgres implementation of fraud.Repository
// (S72 — promotes the S68 memory stub to durable storage).
//
// Save is upsert on booking_id: a re-Assess (the same booking scored
// again, e.g. after a modification) replaces the previous row rather
// than appending. Mirrors the memory adapter's behaviour and matches
// the UNIQUE constraint on booking_id at the schema edge.
type FraudRepository struct {
	pool *pgxpool.Pool
}

func NewFraudRepository(pool *pgxpool.Pool) *FraudRepository {
	return &FraudRepository{pool: pool}
}

var _ fraud.Repository = (*FraudRepository)(nil)

const fraudColumns = `id, booking_id, guest_id, score, level, signals, created_at`

func (r *FraudRepository) Save(ctx context.Context, a *fraud.Assessment) error {
	// Signals encode as a JSON array — small enough to round-trip via
	// json.Marshal rather than a typed pgtype. The domain guarantees
	// every signal carries a non-empty name + positive impact, so the
	// schema's JSONB column doesn't need extra validation.
	signals, err := json.Marshal(a.Signals)
	if err != nil {
		return shared.NewValidationError("fraud: signals not json-encodable: " + err.Error())
	}
	if a.Signals == nil {
		signals = []byte("[]")
	}
	// ON CONFLICT (booking_id) DO UPDATE — re-assess overwrites the
	// previous row's id, score, level, signals, created_at. The id is
	// updated so the latest row's id matches the domain object's id
	// (callers that captured the original id would otherwise get a
	// 404 on FindByBookingID for the booking they just scored).
	_, err = r.pool.Exec(ctx, `
		INSERT INTO fraud_assessments (`+fraudColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (booking_id) DO UPDATE SET
			id         = EXCLUDED.id,
			guest_id   = EXCLUDED.guest_id,
			score      = EXCLUDED.score,
			level      = EXCLUDED.level,
			signals    = EXCLUDED.signals,
			created_at = EXCLUDED.created_at`,
		a.ID, a.BookingID, a.GuestID, a.Score, string(a.Level), signals, a.CreatedAt,
	)
	return mapError(err)
}

func (r *FraudRepository) FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*fraud.Assessment, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+fraudColumns+` FROM fraud_assessments WHERE booking_id = $1`, bookingID)
	var (
		a       fraud.Assessment
		level   string
		signals []byte
	)
	if err := row.Scan(&a.ID, &a.BookingID, &a.GuestID, &a.Score, &level, &signals, &a.CreatedAt); err != nil {
		return nil, mapError(err)
	}
	a.Level = fraud.Level(level)
	if err := json.Unmarshal(signals, &a.Signals); err != nil {
		return nil, shared.NewValidationError("fraud: signals not json-decodable: " + err.Error())
	}
	return &a, nil
}

// AnonymizeByGuest zeroes the guest_id link on every assessment the
// user triggered. The assessment row is RETAINED as the forensic
// record tied to the booking — admins still see "this booking was
// flagged high-risk", just no longer with a direct path back to the
// (now anonymised) account.
func (r *FraudRepository) AnonymizeByGuest(ctx context.Context, guestID uuid.UUID) (int, error) {
	ct, err := r.pool.Exec(ctx,
		`UPDATE fraud_assessments SET guest_id = $2 WHERE guest_id = $1`,
		guestID, uuid.Nil,
	)
	if err != nil {
		return 0, mapError(err)
	}
	return int(ct.RowsAffected()), nil
}

func (r *FraudRepository) List(ctx context.Context, f fraud.ListFilter, page shared.Page) (shared.PageResult[*fraud.Assessment], error) {
	// MinLevel implements "at least this severe". We compile the
	// filter into an IN-list rather than a numeric rank column —
	// only 3 values, the planner picks the (level, created_at) idx
	// either way.
	var where string
	args := make([]any, 0, 4)
	if f.MinLevel.Valid() {
		switch f.MinLevel {
		case fraud.LevelLow:
			// no-op — every row qualifies
		case fraud.LevelMedium:
			where = " WHERE level IN ($1, $2)"
			args = append(args, string(fraud.LevelMedium), string(fraud.LevelHigh))
		case fraud.LevelHigh:
			where = " WHERE level = $1"
			args = append(args, string(fraud.LevelHigh))
		}
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM fraud_assessments`+where, args...).Scan(&total); err != nil {
		return shared.PageResult[*fraud.Assessment]{}, mapError(err)
	}

	// ORDER BY (created_at DESC, id ASC) matches the memory adapter
	// so swapping the backing store doesn't reshuffle pages.
	rows, err := r.pool.Query(ctx,
		`SELECT `+fraudColumns+` FROM fraud_assessments`+where+
			` ORDER BY created_at DESC, id ASC LIMIT $`+itoa(len(args)+1)+
			` OFFSET $`+itoa(len(args)+2),
		append(args, page.Limit, page.Offset)...,
	)
	if err != nil {
		return shared.PageResult[*fraud.Assessment]{}, mapError(err)
	}
	defer rows.Close()

	out := make([]*fraud.Assessment, 0)
	for rows.Next() {
		var (
			a       fraud.Assessment
			level   string
			signals []byte
		)
		if err := rows.Scan(&a.ID, &a.BookingID, &a.GuestID, &a.Score, &level, &signals, &a.CreatedAt); err != nil {
			return shared.PageResult[*fraud.Assessment]{}, mapError(err)
		}
		a.Level = fraud.Level(level)
		if err := json.Unmarshal(signals, &a.Signals); err != nil {
			return shared.PageResult[*fraud.Assessment]{}, shared.NewValidationError("fraud: signals not json-decodable: " + err.Error())
		}
		out = append(out, &a)
	}
	if err := rows.Err(); err != nil {
		return shared.PageResult[*fraud.Assessment]{}, mapError(err)
	}
	return shared.PageResult[*fraud.Assessment]{Items: out, Total: total}, nil
}
