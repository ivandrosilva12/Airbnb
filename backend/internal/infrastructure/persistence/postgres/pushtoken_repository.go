package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/pushtoken"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PushTokenRepository is the Postgres implementation of pushtoken.Repository.
// Save upserts on (platform, token) so the same physical device re-registering
// only refreshes last_seen and reassigns ownership if the device switched
// accounts.
type PushTokenRepository struct {
	pool *pgxpool.Pool
}

// NewPushTokenRepository builds a PushTokenRepository.
func NewPushTokenRepository(pool *pgxpool.Pool) *PushTokenRepository {
	return &PushTokenRepository{pool: pool}
}

func (r *PushTokenRepository) Save(ctx context.Context, t *pushtoken.Token) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO push_tokens (id, user_id, platform, token, last_seen, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (platform, token) DO UPDATE
			SET user_id = EXCLUDED.user_id,
			    last_seen = EXCLUDED.last_seen`,
		t.ID, t.UserID, string(t.Platform), t.Token, t.LastSeen, t.CreatedAt,
	)
	return mapError(err)
}

func (r *PushTokenRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]*pushtoken.Token, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, platform, token, last_seen, created_at
		FROM push_tokens
		WHERE user_id = $1
		ORDER BY last_seen DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []*pushtoken.Token
	for rows.Next() {
		var (
			t        pushtoken.Token
			platform string
		)
		if err := rows.Scan(&t.ID, &t.UserID, &platform, &t.Token, &t.LastSeen, &t.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		t.Platform = pushtoken.Platform(platform)
		out = append(out, &t)
	}
	return out, mapError(rows.Err())
}

func (r *PushTokenRepository) DeleteByToken(ctx context.Context, platform pushtoken.Platform, token string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM push_tokens WHERE platform=$1 AND token=$2`,
		string(platform), token,
	)
	return mapError(err)
}

func (r *PushTokenRepository) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM push_tokens WHERE user_id=$1`, userID)
	return mapError(err)
}
