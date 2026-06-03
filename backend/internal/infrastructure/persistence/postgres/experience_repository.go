package postgres

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/airhost/backend/internal/domain/experience"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExperienceRepository is the Postgres implementation of
// experience.Repository (S75 — promotes the S71 memory stub to durable
// storage). Behaviour matches the memory adapter so swapping the
// backing store doesn't reshuffle paged views or filter semantics.
type ExperienceRepository struct {
	pool *pgxpool.Pool
}

func NewExperienceRepository(pool *pgxpool.Pool) *ExperienceRepository {
	return &ExperienceRepository{pool: pool}
}

var _ experience.Repository = (*ExperienceRepository)(nil)

// experienceColumns lists every column in the order the
// Scan/INSERT pairs expect — kept as a const so a column-rename touches
// one spot.
const experienceColumns = `id, host_id, title, description, category, status,
	city, country, latitude, longitude,
	duration_minutes, max_guests, price_amount_cents, price_currency, language,
	photos, created_at, updated_at`

func (r *ExperienceRepository) Create(ctx context.Context, e *experience.Experience) error {
	photos, err := marshalExperiencePhotos(e.Photos)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO experiences (`+experienceColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		e.ID, e.HostID, e.Title, e.Description, string(e.Category), string(e.Status),
		e.Address.City, e.Address.Country, e.Address.Latitude, e.Address.Longitude,
		e.DurationMinutes, e.MaxGuests, e.PricePerGuest.AmountCents(), e.PricePerGuest.Currency(), e.Language,
		photos, e.CreatedAt, e.UpdatedAt,
	)
	return mapError(err)
}

func (r *ExperienceRepository) Update(ctx context.Context, e *experience.Experience) error {
	photos, err := marshalExperiencePhotos(e.Photos)
	if err != nil {
		return err
	}
	// host_id and created_at are immutable post-Create; everything else
	// can move under the domain's UpdateBasics / Publish / Suspend /
	// AddPhoto methods.
	tag, err := r.pool.Exec(ctx, `
		UPDATE experiences SET
			title              = $2,
			description        = $3,
			category           = $4,
			status             = $5,
			city               = $6,
			country            = $7,
			latitude           = $8,
			longitude          = $9,
			duration_minutes   = $10,
			max_guests         = $11,
			price_amount_cents = $12,
			price_currency     = $13,
			language           = $14,
			photos             = $15,
			updated_at         = $16
		WHERE id = $1`,
		e.ID, e.Title, e.Description, string(e.Category), string(e.Status),
		e.Address.City, e.Address.Country, e.Address.Latitude, e.Address.Longitude,
		e.DurationMinutes, e.MaxGuests, e.PricePerGuest.AmountCents(), e.PricePerGuest.Currency(), e.Language,
		photos, e.UpdatedAt,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *ExperienceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM experiences WHERE id = $1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *ExperienceRepository) FindByID(ctx context.Context, id uuid.UUID) (*experience.Experience, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+experienceColumns+` FROM experiences WHERE id = $1`, id)
	return scanExperience(row)
}

func (r *ExperienceRepository) ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*experience.Experience], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM experiences WHERE host_id = $1`, hostID,
	).Scan(&total); err != nil {
		return shared.PageResult[*experience.Experience]{}, mapError(err)
	}

	// ORDER BY (created_at DESC, id ASC) matches the memory adapter so
	// swapping the backing store doesn't reshuffle the host dashboard.
	rows, err := r.pool.Query(ctx,
		`SELECT `+experienceColumns+` FROM experiences
		 WHERE host_id = $1
		 ORDER BY created_at DESC, id ASC
		 LIMIT $2 OFFSET $3`,
		hostID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*experience.Experience]{}, mapError(err)
	}
	defer rows.Close()

	out, err := scanExperienceRows(rows)
	if err != nil {
		return shared.PageResult[*experience.Experience]{}, err
	}
	return shared.PageResult[*experience.Experience]{Items: out, Total: total}, nil
}

func (r *ExperienceRepository) Search(ctx context.Context, f experience.SearchFilter, page shared.Page) (shared.PageResult[*experience.Experience], error) {
	// Search is the catalogue path — drafts and suspended listings are
	// invisible regardless of the filter. The mandatory status='published'
	// predicate hits the partial idx_experiences_search index.
	where := ` WHERE status = 'published'`
	args := make([]any, 0, 5)

	if f.Category != "" {
		args = append(args, string(f.Category))
		where += ` AND category = $` + itoa(len(args))
	}
	if city := strings.TrimSpace(f.City); city != "" {
		// Case-insensitive substring match (mirrors memory adapter).
		args = append(args, "%"+strings.ToLower(city)+"%")
		where += ` AND lower(city) LIKE $` + itoa(len(args))
	}
	if country := strings.TrimSpace(f.Country); country != "" {
		args = append(args, "%"+strings.ToLower(country)+"%")
		where += ` AND lower(country) LIKE $` + itoa(len(args))
	}
	if lang := strings.TrimSpace(f.Language); lang != "" {
		args = append(args, strings.ToLower(lang))
		where += ` AND language = $` + itoa(len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM experiences`+where, args...,
	).Scan(&total); err != nil {
		return shared.PageResult[*experience.Experience]{}, mapError(err)
	}

	limitIdx := itoa(len(args) + 1)
	offsetIdx := itoa(len(args) + 2)
	args = append(args, page.Limit, page.Offset)

	rows, err := r.pool.Query(ctx,
		`SELECT `+experienceColumns+` FROM experiences`+where+
			` ORDER BY created_at DESC, id ASC LIMIT $`+limitIdx+` OFFSET $`+offsetIdx,
		args...,
	)
	if err != nil {
		return shared.PageResult[*experience.Experience]{}, mapError(err)
	}
	defer rows.Close()

	out, err := scanExperienceRows(rows)
	if err != nil {
		return shared.PageResult[*experience.Experience]{}, err
	}
	return shared.PageResult[*experience.Experience]{Items: out, Total: total}, nil
}

// marshalExperiencePhotos serialises the photo slice into the JSONB
// payload. nil slice → "[]" so the column NOT NULL DEFAULT holds even
// for a freshly-created draft with no photos.
func marshalExperiencePhotos(photos []experience.Photo) ([]byte, error) {
	if len(photos) == 0 {
		return []byte("[]"), nil
	}
	out, err := json.Marshal(photos)
	if err != nil {
		return nil, shared.NewValidationError("experience: photos not json-encodable: " + err.Error())
	}
	return out, nil
}

// pgxScanner abstracts pgx.Row / pgx.Rows so the per-row hydration logic
// lives in one place.
type pgxScanner interface {
	Scan(dest ...any) error
}

func scanExperience(s pgxScanner) (*experience.Experience, error) {
	var (
		e            experience.Experience
		category     string
		status       string
		priceCents   int64
		priceCurr    string
		photosJSON   []byte
	)
	if err := s.Scan(
		&e.ID, &e.HostID, &e.Title, &e.Description, &category, &status,
		&e.Address.City, &e.Address.Country, &e.Address.Latitude, &e.Address.Longitude,
		&e.DurationMinutes, &e.MaxGuests, &priceCents, &priceCurr, &e.Language,
		&photosJSON, &e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		return nil, mapError(err)
	}
	e.Category = experience.Category(category)
	e.Status = experience.Status(status)
	price, err := shared.NewMoney(priceCents, priceCurr)
	if err != nil {
		return nil, shared.NewValidationError("experience: stored price invalid: " + err.Error())
	}
	e.PricePerGuest = price
	if len(photosJSON) > 0 {
		if err := json.Unmarshal(photosJSON, &e.Photos); err != nil {
			return nil, shared.NewValidationError("experience: photos not json-decodable: " + err.Error())
		}
	}
	return &e, nil
}

// rowsScanner is the slice-iteration shape returned by pool.Query.
type rowsScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanExperienceRows(rows rowsScanner) ([]*experience.Experience, error) {
	out := make([]*experience.Experience, 0)
	for rows.Next() {
		e, err := scanExperience(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return out, nil
}
