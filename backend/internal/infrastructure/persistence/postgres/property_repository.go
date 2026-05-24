package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	cancellation_policy, average_rating, review_count,
	weekly_discount_pct, monthly_discount_pct, tax_rate_pct, instant_book, created_at, updated_at`

func (r *PropertyRepository) Create(ctx context.Context, p *property.Property) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO properties (`+propertyColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29)`,
		p.ID, p.HostID, p.Title, p.Description, string(p.Type), string(p.Status),
		p.Address.Line1, p.Address.City, p.Address.Country, p.Address.PostalCode,
		p.Address.Latitude, p.Address.Longitude,
		p.PricePerNight.AmountCents(), p.PricePerNight.Currency(), p.CleaningFee.AmountCents(),
		p.MaxGuests, p.Bedrooms, p.Beds, p.Bathrooms, p.Amenities,
		string(p.CancellationPolicy), p.AverageRating, p.ReviewCount,
		p.PricingPolicy.WeeklyDiscountPct, p.PricingPolicy.MonthlyDiscountPct, p.PricingPolicy.TaxRatePct,
		p.InstantBook, p.CreatedAt, p.UpdatedAt,
	)
	return mapError(err)
}

// orderBy maps a Sort to a safe ORDER BY clause (whitelisted, never user input).
func orderBy(s property.Sort) string {
	switch s {
	case property.SortPriceAsc:
		return "price_cents ASC, created_at DESC"
	case property.SortPriceDesc:
		return "price_cents DESC, created_at DESC"
	case property.SortRating:
		return "average_rating DESC, review_count DESC, created_at DESC"
	default:
		return "created_at DESC"
	}
}

func (r *PropertyRepository) UpdateRating(ctx context.Context, id uuid.UUID, average float64, count int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE properties SET average_rating=$2, review_count=$3 WHERE id=$1`, id, average, count)
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
			amenities=$19, cancellation_policy=$20,
			weekly_discount_pct=$21, monthly_discount_pct=$22, tax_rate_pct=$23, instant_book=$24, updated_at=$25
		WHERE id=$1`,
		p.ID, p.Title, p.Description, string(p.Type), string(p.Status),
		p.Address.Line1, p.Address.City, p.Address.Country, p.Address.PostalCode,
		p.Address.Latitude, p.Address.Longitude,
		p.PricePerNight.AmountCents(), p.PricePerNight.Currency(), p.CleaningFee.AmountCents(),
		p.MaxGuests, p.Bedrooms, p.Beds, p.Bathrooms, p.Amenities, string(p.CancellationPolicy),
		p.PricingPolicy.WeeklyDiscountPct, p.PricingPolicy.MonthlyDiscountPct, p.PricingPolicy.TaxRatePct, p.InstantBook, p.UpdatedAt,
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

func (r *PropertyRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]*property.Property, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+propertyColumns+` FROM properties WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, mapError(err)
	}
	res, err := r.collect(ctx, rows, 0)
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (r *PropertyRepository) ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*property.Property], error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM properties WHERE host_id=$1`, hostID).Scan(&total); err != nil {
		return shared.PageResult[*property.Property]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+propertyColumns+` FROM properties
		WHERE host_id=$1 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		hostID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*property.Property]{}, mapError(err)
	}
	return r.collect(ctx, rows, total)
}

func (r *PropertyRepository) Search(ctx context.Context, c property.SearchCriteria) (shared.PageResult[*property.Property], error) {
	return r.search(ctx, c, nil)
}

type availabilityWindow struct{ checkIn, checkOut time.Time }

// SearchAvailable runs Search and additionally excludes listings booked or
// host-blocked over [checkIn, checkOut), pushing that filter into the database
// as NOT EXISTS subqueries instead of materialising the occupied id set in the
// application. It implements property.AvailabilitySearcher.
func (r *PropertyRepository) SearchAvailable(ctx context.Context, c property.SearchCriteria, checkIn, checkOut time.Time) (shared.PageResult[*property.Property], error) {
	return r.search(ctx, c, &availabilityWindow{checkIn: checkIn, checkOut: checkOut})
}

func (r *PropertyRepository) search(ctx context.Context, c property.SearchCriteria, win *availabilityWindow) (shared.PageResult[*property.Property], error) {
	var (
		conds = []string{"status = 'published'"}
		args  []any
	)
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if c.Query != "" {
		args = append(args, c.Query)
		n := len(args)
		conds = append(conds, fmt.Sprintf(
			"(title ILIKE '%%' || $%d || '%%' OR description ILIKE '%%' || $%d || '%%' OR city ILIKE '%%' || $%d || '%%')",
			n, n, n))
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
	if c.Geo != nil && c.Geo.RadiusKm > 0 {
		// Great-circle (haversine) distance in km. acos's argument is clamped to
		// [-1, 1] to avoid NaN from floating-point error at tiny distances.
		lat := len(args) + 1
		lng := len(args) + 2
		radius := len(args) + 3
		args = append(args, c.Geo.Lat, c.Geo.Lng, c.Geo.RadiusKm)
		conds = append(conds, fmt.Sprintf(
			`6371 * acos(least(1, greatest(-1, `+
				`cos(radians($%d)) * cos(radians(latitude)) * cos(radians(longitude) - radians($%d)) `+
				`+ sin(radians($%d)) * sin(radians(latitude))))) <= $%d`,
			lat, lng, lat, radius))
	}
	if win != nil {
		// Push availability filtering into the query: keep only listings with no
		// active booking and no host block overlapping [checkIn, checkOut). The
		// half-open overlap test (check_in < end AND start < check_out) matches the
		// booking/block availability queries and the no-overlap DB constraint.
		args = append(args, win.checkIn, win.checkOut)
		ci := len(args) - 1
		co := len(args)
		conds = append(conds, fmt.Sprintf(
			`NOT EXISTS (SELECT 1 FROM bookings b WHERE b.property_id = properties.id `+
				`AND b.status IN ('pending','confirmed') AND b.check_in < $%d AND $%d < b.check_out)`, co, ci))
		conds = append(conds, fmt.Sprintf(
			`NOT EXISTS (SELECT 1 FROM property_blocks pb WHERE pb.property_id = properties.id `+
				`AND pb.check_in < $%d AND $%d < pb.check_out)`, co, ci))
	}
	where := "WHERE " + strings.Join(conds, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM properties `+where, args...).Scan(&total); err != nil {
		return shared.PageResult[*property.Property]{}, mapError(err)
	}

	args = append(args, c.Page.Limit, c.Page.Offset)
	query := fmt.Sprintf(`SELECT %s FROM properties %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		propertyColumns, where, orderBy(c.Sort), len(args)-1, len(args))
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
		policy        string
	)
	err := row.Scan(
		&p.ID, &p.HostID, &p.Title, &p.Description, &pType, &status,
		&p.Address.Line1, &p.Address.City, &p.Address.Country, &p.Address.PostalCode,
		&p.Address.Latitude, &p.Address.Longitude,
		&priceCents, &currency, &cleaningCents, &p.MaxGuests, &p.Bedrooms, &p.Beds, &p.Bathrooms, &p.Amenities,
		&policy, &p.AverageRating, &p.ReviewCount,
		&p.PricingPolicy.WeeklyDiscountPct, &p.PricingPolicy.MonthlyDiscountPct, &p.PricingPolicy.TaxRatePct,
		&p.InstantBook, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.Type = property.PropertyType(pType)
	p.Status = property.Status(status)
	p.CancellationPolicy = property.CancellationPolicy(policy)
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
