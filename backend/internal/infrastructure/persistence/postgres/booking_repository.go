package postgres

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BookingRepository is the Postgres implementation of booking.Repository.
type BookingRepository struct {
	pool *pgxpool.Pool
}

// NewBookingRepository builds a BookingRepository.
func NewBookingRepository(pool *pgxpool.Pool) *BookingRepository {
	return &BookingRepository{pool: pool}
}

const bookingColumns = `id, property_id, guest_id, check_in, check_out, guests,
	total_cents, currency, status, created_at, updated_at`

func (r *BookingRepository) Create(ctx context.Context, b *booking.Booking) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO bookings (`+bookingColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		b.ID, b.PropertyID, b.GuestID, b.Dates.CheckIn, b.Dates.CheckOut, b.Guests,
		b.TotalPrice.AmountCents(), b.TotalPrice.Currency(), string(b.Status), b.CreatedAt, b.UpdatedAt,
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

func (r *BookingRepository) list(ctx context.Context, where string, arg any, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM bookings WHERE `+where, arg).Scan(&total); err != nil {
		return shared.PageResult[*booking.Booking]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+bookingColumns+` FROM bookings WHERE `+where+`
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
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
		b          booking.Booking
		status     string
		totalCents int64
		currency   string
	)
	err := row.Scan(
		&b.ID, &b.PropertyID, &b.GuestID, &b.Dates.CheckIn, &b.Dates.CheckOut, &b.Guests,
		&totalCents, &currency, &status, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	b.Status = booking.Status(status)
	money, _ := shared.NewMoney(totalCents, currency)
	b.TotalPrice = money
	return &b, nil
}
