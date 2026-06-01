package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CohostRepository is the Postgres implementation of property.CohostRepository.
type CohostRepository struct {
	pool *pgxpool.Pool
}

// NewCohostRepository builds a CohostRepository.
func NewCohostRepository(pool *pgxpool.Pool) *CohostRepository {
	return &CohostRepository{pool: pool}
}

var _ property.CohostRepository = (*CohostRepository)(nil)

const cohostColumns = `id, property_id, user_id, permissions, created_at, updated_at`

// permissionsToText flattens the typed slice for the TEXT[] column. The empty
// slice maps to an empty array — never to NULL — to match the column default
// and keep round-trips lossless.
func permissionsToText(perms []property.CohostPermission) []string {
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, string(p))
	}
	return out
}

// permissionsFromText reverses permissionsToText. Unknown values are kept as
// typed permissions (the domain has authority on what's valid for new grants;
// any stale row that survives a code change still surfaces here verbatim).
func permissionsFromText(in []string) []property.CohostPermission {
	out := make([]property.CohostPermission, 0, len(in))
	for _, p := range in {
		out = append(out, property.CohostPermission(p))
	}
	return out
}

func (r *CohostRepository) Create(ctx context.Context, c *property.Cohost) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO cohosts (`+cohostColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		c.ID, c.PropertyID, c.UserID, permissionsToText(c.Permissions), c.CreatedAt, c.UpdatedAt,
	)
	return mapError(err)
}

func (r *CohostRepository) Update(ctx context.Context, c *property.Cohost) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE cohosts
		   SET permissions=$2, updated_at=$3
		 WHERE id=$1`,
		c.ID, permissionsToText(c.Permissions), c.UpdatedAt,
	)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *CohostRepository) Delete(ctx context.Context, id uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM cohosts WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *CohostRepository) FindByID(ctx context.Context, id uuid.UUID) (*property.Cohost, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+cohostColumns+` FROM cohosts WHERE id=$1`, id)
	c, err := scanCohost(row)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *CohostRepository) FindByPropertyAndUser(ctx context.Context, propertyID, userID uuid.UUID) (*property.Cohost, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+cohostColumns+` FROM cohosts WHERE property_id=$1 AND user_id=$2`,
		propertyID, userID,
	)
	c, err := scanCohost(row)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *CohostRepository) ListByProperty(ctx context.Context, propertyID uuid.UUID) ([]*property.Cohost, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+cohostColumns+` FROM cohosts WHERE property_id=$1 ORDER BY created_at`,
		propertyID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	out := make([]*property.Cohost, 0)
	for rows.Next() {
		c, err := scanCohostRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, mapError(rows.Err())
}

func (r *CohostRepository) ListPropertiesForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT property_id FROM cohosts WHERE user_id=$1`,
		userID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	out := make([]uuid.UUID, 0)
	for rows.Next() {
		var pid uuid.UUID
		if err := rows.Scan(&pid); err != nil {
			return nil, mapError(err)
		}
		out = append(out, pid)
	}
	return out, mapError(rows.Err())
}

// scanCohost reads a single row from a QueryRow result.
func scanCohost(row interface{ Scan(...any) error }) (*property.Cohost, error) {
	var c property.Cohost
	var perms []string
	if err := row.Scan(&c.ID, &c.PropertyID, &c.UserID, &perms, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, mapError(err)
	}
	c.Permissions = permissionsFromText(perms)
	return &c, nil
}

// scanCohostRow is the equivalent for a rows iterator.
func scanCohostRow(rows interface{ Scan(...any) error }) (*property.Cohost, error) {
	return scanCohost(rows)
}
