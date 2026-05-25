package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRepository is the Postgres implementation of user.Repository.
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository builds a UserRepository.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

const userColumns = `id, keycloak_sub, email, full_name, role, avatar_url, is_active,
	email_opt_bookings, email_opt_messages, payout_account_id, payouts_enabled, created_at, updated_at`

func (r *UserRepository) Create(ctx context.Context, u *user.User) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (`+userColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		u.ID, u.KeycloakSub, u.Email, u.FullName, string(u.Role), u.AvatarURL, u.IsActive,
		u.EmailPrefs.Bookings, u.EmailPrefs.Messages, u.PayoutAccountID, u.PayoutsEnabled, u.CreatedAt, u.UpdatedAt,
	)
	return mapError(err)
}

func (r *UserRepository) Update(ctx context.Context, u *user.User) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users
		SET email=$2, full_name=$3, role=$4, avatar_url=$5, is_active=$6,
			email_opt_bookings=$7, email_opt_messages=$8, payout_account_id=$9, payouts_enabled=$10, updated_at=$11
		WHERE id=$1`,
		u.ID, u.Email, u.FullName, string(u.Role), u.AvatarURL, u.IsActive,
		u.EmailPrefs.Bookings, u.EmailPrefs.Messages, u.PayoutAccountID, u.PayoutsEnabled, u.UpdatedAt,
	)
	return mapError(err)
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	return r.findBy(ctx, `WHERE id=$1`, id)
}

func (r *UserRepository) FindByKeycloakSub(ctx context.Context, sub string) (*user.User, error) {
	return r.findBy(ctx, `WHERE keycloak_sub=$1`, sub)
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*user.User, error) {
	return r.findBy(ctx, `WHERE email=$1`, email)
}

func (r *UserRepository) FindByPayoutAccountID(ctx context.Context, accountID string) (*user.User, error) {
	return r.findBy(ctx, `WHERE payout_account_id=$1`, accountID)
}

func (r *UserRepository) findBy(ctx context.Context, where string, arg any) (*user.User, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users `+where, arg)
	var (
		u    user.User
		role string
	)
	err := row.Scan(&u.ID, &u.KeycloakSub, &u.Email, &u.FullName, &role, &u.AvatarURL, &u.IsActive,
		&u.EmailPrefs.Bookings, &u.EmailPrefs.Messages, &u.PayoutAccountID, &u.PayoutsEnabled, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	u.Role = user.Role(role)
	return &u, nil
}
