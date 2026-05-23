package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PropertyRepository is the Postgres implementation of property.Repository.
type PropertyRepository struct {
	pool *pgxpool.Pool
}

// NewPropertyRepository builds a PropertyRepository.
func NewPropertyRepository(pool *pgxpool.Pool) *PropertyRepository {
	return &PropertyRepository{pool: pool}
}

const propertyColumns = `id, host_id, title, description, type, status,
	address_line1, city, country, postal_code, latitude, longitude,
	price_cents, currency, cleaning_fee_cents, max_guests, bedrooms, beds, bathrooms, amenities,
	created_at, updated_at`

func (r *PropertyRepository) Create(ctx context.Context, p *property.Property) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO properties (`+propertyColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
		p.ID, p.HostID, p.Title, p.Description, string(p.Type), string(p.Status),
		p.Address.Line1, p.Address.City, p.Address.Country, p.Address.PostalCode,
		p.Address.Latitude, p.Address.Longitude,
		p.PricePerNight.AmountCents(), p.PricePerNight.Currency(), p.CleaningFee.AmountCents(),
		p.MaxGuests, p.Bedrooms, p.Beds, p.Bathrooms, p.Amenities,
		p.CreatedAt, p.UpdatedAt,
	)
	return mapError(err)
}

func (r *PropertyRepository) Update(ctx context.Context, p *property.Property) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mapError(err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE properties SET
			title=$2, description=$3, type=$4, status=$5,
			address_line1=$6, city=$7, country=$8, postal_code=$9, latitude=$10, longitude=$11,
			price_cents=$12, currency=$13, cleaning_fee_cents=$14, max_guests=$15, bedrooms=$16, beds=$17, bathrooms=$18,
			amenities=$19, updated_at=$20
		WHERE id=$1`,
		p.ID, p.Title, p.Description, string(p.Type), string(p.Status),
		p.Address.Line1, p.Address.City, p.Address.Country, p.Address.PostalCode,
		p.Address.Latitude, p.Address.Longitude,
		p.PricePerNight.AmountCents(), p.PricePerNight.Currency(), p.CleaningFee.AmountCents(),
		p.MaxGuests, p.Bedrooms, p.Beds, p.Bathrooms, p.Amenities, p.UpdatedAt,
	)
	if err != nil {
		return mapError(err)
	}

	// Re-sync photos: simplest correct strategy is replace-all within the tx.
	if _, err := tx.Exec(ctx, `DELETE FROM property_photos WHERE property_id=$1`, p.ID); err != nil {
		return mapError(err)
	}
	for _, ph := range p.Photos {
		if _, err := tx.Exec(ctx, `
			INSERT INTO property_photos (id, property_id, object_key, url, position)
			VALUES ($1,$2,$3,$4,$5)`,
			ph.ID, p.ID, ph.ObjectKey, ph.URL, ph.Position,
		); err != nil {
			return mapError(err)
		}
	}
	return mapError(tx.Commit(ctx))
}

func (r *PropertyRepository) Delete(ctx context.Context, id uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM properties WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *PropertyRepository) FindByID(ctx context.Context, id uuid.UUID) (*property.Property, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+propertyColumns+` FROM properties WHERE id=$1`, id)
	p, err := scanProperty(row)
	if err != nil {
		return nil, mapError(err)
	}
	photos, err := r.loadPhotos(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	p.Photos = photos[id]
	return p, nil
}

func (r *PropertyRepository) ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*property.Property], error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM properties WHERE host_id=$1`, hostID).Scan(&total); err != nil {
		return shared.PageResult[*property.Property]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+propertyColumns+` FROM properties
		WHERE host_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		hostID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*property.Property]{}, mapError(err)
	}
	return r.collect(ctx, rows, total)
}

func (r *PropertyRepository) Search(ctx context.Context, c property.SearchCriteria) (shared.PageResult[*property.Property], error) {
	var (
		conds = []string{"status = 'published'"}
		args  []any
	)
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if c.City != "" {
		add("city ILIKE '%%' || $%d || '%%'", c.City)
	}
	if c.Country != "" {
		add("country ILIKE $%d", c.Country)
	}
	if c.Type != "" {
		add("type = $%d", string(c.Type))
	}
	if c.MinGuests > 0 {
		add("max_guests >= $%d", c.MinGuests)
	}
	if c.MaxPrice > 0 {
		add("price_cents <= $%d", c.MaxPrice)
	}
	if len(c.Amenities) > 0 {
		add("amenities @> $%d", c.Amenities)
	}
	if len(c.ExcludeIDs) > 0 {
		add("id <> ALL($%d)", c.ExcludeIDs)
	}
	where := "WHERE " + strings.Join(conds, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM properties `+where, args...).Scan(&total); err != nil {
		return shared.PageResult[*property.Property]{}, mapError(err)
	}

	args = append(args, c.Page.Limit, c.Page.Offset)
	query := fmt.Sprintf(`SELECT %s FROM properties %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		propertyColumns, where, len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return shared.PageResult[*property.Property]{}, mapError(err)
	}
	return r.collect(ctx, rows, total)
}

// collect scans property rows then batch-loads their photos.
func (r *PropertyRepository) collect(ctx context.Context, rows pgx.Rows, total int64) (shared.PageResult[*property.Property], error) {
	defer rows.Close()
	var (
		props []*property.Property
		ids   []uuid.UUID
	)
	for rows.Next() {
		p, err := scanProperty(rows)
		if err != nil {
			return shared.PageResult[*property.Property]{}, mapError(err)
		}
		props = append(props, p)
		ids = append(ids, p.ID)
	}
	if err := rows.Err(); err != nil {
		return shared.PageResult[*property.Property]{}, mapError(err)
	}

	photos, err := r.loadPhotos(ctx, ids)
	if err != nil {
		return shared.PageResult[*property.Property]{}, err
	}
	for _, p := range props {
		p.Photos = photos[p.ID]
	}
	return shared.PageResult[*property.Property]{Items: props, Total: total}, nil
}

func (r *PropertyRepository) loadPhotos(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]property.Photo, error) {
	out := make(map[uuid.UUID][]property.Photo)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, property_id, object_key, url, position
		FROM property_photos WHERE property_id = ANY($1) ORDER BY position ASC`, ids)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			ph  property.Photo
			pid uuid.UUID
		)
		if err := rows.Scan(&ph.ID, &pid, &ph.ObjectKey, &ph.URL, &ph.Position); err != nil {
			return nil, mapError(err)
		}
		out[pid] = append(out[pid], ph)
	}
	return out, mapError(rows.Err())
}

// rowScanner abstracts pgx.Row and pgx.Rows for shared scanning logic.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanProperty(row rowScanner) (*property.Property, error) {
	var (
		p             property.Property
		pType         string
		status        string
		priceCents    int64
		currency      string
		cleaningCents int64
	)
	err := row.Scan(
		&p.ID, &p.HostID, &p.Title, &p.Description, &pType, &status,
		&p.Address.Line1, &p.Address.City, &p.Address.Country, &p.Address.PostalCode,
		&p.Address.Latitude, &p.Address.Longitude,
		&priceCents, &currency, &cleaningCents, &p.MaxGuests, &p.Bedrooms, &p.Beds, &p.Bathrooms, &p.Amenities,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.Type = property.PropertyType(pType)
	p.Status = property.Status(status)
	money, _ := shared.NewMoney(priceCents, currency)
	p.PricePerNight = money
	cleaning, _ := shared.NewMoney(cleaningCents, currency)
	p.CleaningFee = cleaning
	if p.Amenities == nil {
		p.Amenities = []string{}
	}
	p.Photos = []property.Photo{}
	return &p, nil
}
