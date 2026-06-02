package postgres

import (
	"context"
	"errors"

	"github.com/airhost/backend/internal/domain/houserules"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HouseRulesRepository is the Postgres implementation of
// houserules.Repository. Rules are stored append-only by version
// (matching the memory implementation); acceptances are keyed by
// bookingID so a retry doesn't double-insert.
type HouseRulesRepository struct {
	pool *pgxpool.Pool
}

func NewHouseRulesRepository(pool *pgxpool.Pool) *HouseRulesRepository {
	return &HouseRulesRepository{pool: pool}
}

var _ houserules.Repository = (*HouseRulesRepository)(nil)

func (r *HouseRulesRepository) GetCurrent(ctx context.Context, propertyID uuid.UUID) (*houserules.Rules, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT property_id, version, items, updated_at
		  FROM house_rules
		 WHERE property_id = $1
		 ORDER BY version DESC
		 LIMIT 1`, propertyID)
	var out houserules.Rules
	if err := row.Scan(&out.PropertyID, &out.Version, &out.Items, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.ErrNotFound
		}
		return nil, mapError(err)
	}
	return &out, nil
}

func (r *HouseRulesRepository) GetVersion(ctx context.Context, propertyID uuid.UUID, version int) (*houserules.Rules, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT property_id, version, items, updated_at
		  FROM house_rules
		 WHERE property_id = $1 AND version = $2`, propertyID, version)
	var out houserules.Rules
	if err := row.Scan(&out.PropertyID, &out.Version, &out.Items, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.ErrNotFound
		}
		return nil, mapError(err)
	}
	return &out, nil
}

// Save writes a new version row. Relies on the PK (property_id,
// version) to reject a concurrent writer trying to claim the same
// version — the unique-violation surfaces via mapError as
// shared.ErrConflict, matching the memory repo.
func (r *HouseRulesRepository) Save(ctx context.Context, rules *houserules.Rules) error {
	if rules == nil {
		return shared.NewValidationError("houserules: nil rules")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO house_rules (property_id, version, items, updated_at)
		VALUES ($1, $2, $3, $4)`,
		rules.PropertyID, rules.Version, rules.Items, rules.UpdatedAt,
	)
	return mapError(err)
}

// RecordAcceptance is idempotent on booking_id: the ON CONFLICT
// branch returns nil without overwriting the prior row. We DELIBERATELY
// don't update accepted_at/accepted_version on a re-call — the first
// acceptance is the authoritative one for dispute purposes; a retry
// must not silently rewrite it.
func (r *HouseRulesRepository) RecordAcceptance(ctx context.Context, a *houserules.Acceptance) error {
	if a == nil {
		return shared.NewValidationError("houserules: nil acceptance")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO house_rule_acceptances (booking_id, guest_id, property_id, accepted_version, accepted_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (booking_id) DO NOTHING`,
		a.BookingID, a.GuestID, a.PropertyID, a.AcceptedVersion, a.AcceptedAt,
	)
	return mapError(err)
}

func (r *HouseRulesRepository) AcceptanceFor(ctx context.Context, bookingID uuid.UUID) (*houserules.Acceptance, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT booking_id, guest_id, property_id, accepted_version, accepted_at
		  FROM house_rule_acceptances
		 WHERE booking_id = $1`, bookingID)
	var out houserules.Acceptance
	if err := row.Scan(&out.BookingID, &out.GuestID, &out.PropertyID, &out.AcceptedVersion, &out.AcceptedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.ErrNotFound
		}
		return nil, mapError(err)
	}
	return &out, nil
}
