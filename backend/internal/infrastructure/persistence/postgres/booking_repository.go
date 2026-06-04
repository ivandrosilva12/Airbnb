package postgres

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// BookingRepository is the Postgres implementation of booking.Repository. It
// runs against a querier (the pool, or a transaction inside a UnitOfWork).
type BookingRepository struct {
	pool querier
}

// NewBookingRepository builds a BookingRepository.
func NewBookingRepository(db querier) *BookingRepository {
	return &BookingRepository{pool: db}
}

const bookingColumns = `id, property_id, guest_id, check_in, check_out, guests,
	subtotal_cents, discount_cents, cleaning_fee_cents, service_fee_cents, tax_cents,
	total_cents, currency, status, created_at, updated_at`

func (r *BookingRepository) Create(ctx context.Context, b *booking.Booking) error {
	p := b.Pricing
	_, err := r.pool.Exec(ctx, `
		INSERT INTO bookings (`+bookingColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		b.ID, b.PropertyID, b.GuestID, b.Dates.CheckIn, b.Dates.CheckOut, b.Guests,
		p.Subtotal.AmountCents(), p.Discount.AmountCents(), p.CleaningFee.AmountCents(),
		p.ServiceFee.AmountCents(), p.Tax.AmountCents(),
		p.Total.AmountCents(), p.Total.Currency(), string(b.Status), b.CreatedAt, b.UpdatedAt,
	)
	return mapError(err)
}

func (r *BookingRepository) Update(ctx context.Context, b *booking.Booking) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE bookings SET status=$2, updated_at=$3 WHERE id=$1`,
		b.ID, string(b.Status), b.UpdatedAt,
	)
	return mapError(err)
}

func (r *BookingRepository) FindByID(ctx context.Context, id uuid.UUID) (*booking.Booking, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+bookingColumns+` FROM bookings WHERE id=$1`, id)
	b, err := scanBooking(row)
	if err != nil {
		return nil, mapError(err)
	}
	return b, nil
}

func (r *BookingRepository) ListByGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	return r.list(ctx, `guest_id=$1`, guestID, page)
}

func (r *BookingRepository) ListByProperty(ctx context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	return r.list(ctx, `property_id=$1`, propertyID, page)
}

func (r *BookingRepository) ListByPropertyIDs(ctx context.Context, propertyIDs []uuid.UUID, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	if len(propertyIDs) == 0 {
		return shared.PageResult[*booking.Booking]{}, nil
	}
	return r.list(ctx, `property_id = ANY($1)`, propertyIDs, page)
}

func (r *BookingRepository) list(ctx context.Context, where string, arg any, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM bookings WHERE `+where, arg).Scan(&total); err != nil {
		return shared.PageResult[*booking.Booking]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+bookingColumns+` FROM bookings WHERE `+where+`
		ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		arg, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*booking.Booking]{}, mapError(err)
	}
	defer rows.Close()

	var items []*booking.Booking
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return shared.PageResult[*booking.Booking]{}, mapError(err)
		}
		items = append(items, b)
	}
	return shared.PageResult[*booking.Booking]{Items: items, Total: total}, mapError(rows.Err())
}

func (r *BookingRepository) ListActiveInRange(ctx context.Context, propertyID uuid.UUID, from, to time.Time) ([]*booking.Booking, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+bookingColumns+` FROM bookings
		WHERE property_id=$1
		  AND status IN ('pending','confirmed')
		  AND check_in < $3
		  AND $2 < check_out
		ORDER BY check_in ASC`,
		propertyID, from, to,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var items []*booking.Booking
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, mapError(err)
		}
		items = append(items, b)
	}
	return items, mapError(rows.Err())
}

func (r *BookingRepository) BookedPropertyIDs(ctx context.Context, from, to time.Time) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT property_id FROM bookings
		WHERE status IN ('pending','confirmed')
		  AND check_in < $2
		  AND $1 < check_out`,
		from, to,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, mapError(err)
		}
		ids = append(ids, id)
	}
	return ids, mapError(rows.Err())
}

// ListSettledInPeriod returns bookings whose check-out date falls in
// [from, to) and whose status is confirmed or completed (S62). Used by the
// tax-remittance read-model to aggregate per-jurisdiction tax collected in
// a period. Sorted by check_out so a paginated UI can stream rows in order.
func (r *BookingRepository) ListSettledInPeriod(ctx context.Context, from, to time.Time) ([]*booking.Booking, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+bookingColumns+` FROM bookings
		WHERE status IN ('confirmed','completed')
		  AND check_out >= $1
		  AND check_out < $2
		ORDER BY check_out ASC`,
		from, to,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var items []*booking.Booking
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, mapError(err)
		}
		items = append(items, b)
	}
	return items, mapError(rows.Err())
}

// ListConfirmedStartingBetween returns confirmed bookings whose check-in
// falls in [from, to). S102 — drives the arrival-info notification
// scheduler (WF-GAP-007). The check_in column has an index because it's
// used by HasOverlap; the partial filter on status keeps the scan small.
func (r *BookingRepository) ListConfirmedStartingBetween(ctx context.Context, from, to time.Time) ([]*booking.Booking, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+bookingColumns+` FROM bookings
		WHERE status = 'confirmed'
		  AND check_in >= $1
		  AND check_in < $2
		ORDER BY check_in ASC`,
		from, to,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var items []*booking.Booking
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, mapError(err)
		}
		items = append(items, b)
	}
	return items, mapError(rows.Err())
}

func (r *BookingRepository) HasOverlap(ctx context.Context, propertyID uuid.UUID, dates booking.DateRange) (bool, error) {
	var exists bool
	// Half-open overlap: existing.check_in < new.check_out AND new.check_in < existing.check_out.
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM bookings
			WHERE property_id=$1
			  AND status IN ('pending','confirmed')
			  AND check_in < $3
			  AND $2 < check_out
		)`, propertyID, dates.CheckIn, dates.CheckOut,
	).Scan(&exists)
	return exists, mapError(err)
}

func scanBooking(row rowScanner) (*booking.Booking, error) {
	var (
		b             booking.Booking
		status        string
		subtotalCents int64
		discountCents int64
		cleaningCents int64
		serviceCents  int64
		taxCents      int64
		totalCents    int64
		currency      string
	)
	err := row.Scan(
		&b.ID, &b.PropertyID, &b.GuestID, &b.Dates.CheckIn, &b.Dates.CheckOut, &b.Guests,
		&subtotalCents, &discountCents, &cleaningCents, &serviceCents, &taxCents,
		&totalCents, &currency, &status, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	b.Status = booking.Status(status)
	subtotal, _ := shared.NewMoney(subtotalCents, currency)
	discount, _ := shared.NewMoney(discountCents, currency)
	cleaning, _ := shared.NewMoney(cleaningCents, currency)
	service, _ := shared.NewMoney(serviceCents, currency)
	tax, _ := shared.NewMoney(taxCents, currency)
	total, _ := shared.NewMoney(totalCents, currency)
	b.Pricing = booking.Pricing{
		Nights:      b.Dates.Nights(),
		Subtotal:    subtotal,
		Discount:    discount,
		CleaningFee: cleaning,
		ServiceFee:  service,
		Tax:         tax,
		Total:       total,
	}
	return &b, nil
}
