package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReviewRepository is the Postgres implementation of review.Repository.
type ReviewRepository struct {
	pool *pgxpool.Pool
}

// NewReviewRepository builds a ReviewRepository.
func NewReviewRepository(pool *pgxpool.Pool) *ReviewRepository {
	return &ReviewRepository{pool: pool}
}

func (r *ReviewRepository) Create(ctx context.Context, rv *review.Review) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO reviews (id, property_id, booking_id, guest_id, rating, comment, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		rv.ID, rv.PropertyID, rv.BookingID, rv.GuestID, rv.Rating, rv.Comment, rv.CreatedAt,
	)
	return mapError(err)
}

func (r *ReviewRepository) ExistsForBooking(ctx context.Context, bookingID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM reviews WHERE booking_id=$1)`, bookingID,
	).Scan(&exists)
	return exists, mapError(err)
}

func (r *ReviewRepository) ListByProperty(ctx context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*review.Review], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM reviews WHERE property_id=$1`, propertyID,
	).Scan(&total); err != nil {
		return shared.PageResult[*review.Review]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, property_id, booking_id, guest_id, rating, comment, created_at
		FROM reviews WHERE property_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		propertyID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*review.Review]{}, mapError(err)
	}
	defer rows.Close()

	var items []*review.Review
	for rows.Next() {
		var rv review.Review
		if err := rows.Scan(&rv.ID, &rv.PropertyID, &rv.BookingID, &rv.GuestID, &rv.Rating, &rv.Comment, &rv.CreatedAt); err != nil {
			return shared.PageResult[*review.Review]{}, mapError(err)
		}
		items = append(items, &rv)
	}
	return shared.PageResult[*review.Review]{Items: items, Total: total}, mapError(rows.Err())
}

func (r *ReviewRepository) SummaryForProperty(ctx context.Context, propertyID uuid.UUID) (review.Summary, error) {
	var (
		avg   *float64
		count int64
	)
	err := r.pool.QueryRow(ctx,
		`SELECT AVG(rating), COUNT(*) FROM reviews WHERE property_id=$1`, propertyID,
	).Scan(&avg, &count)
	if err != nil {
		return review.Summary{}, mapError(err)
	}
	s := review.Summary{PropertyID: propertyID, Count: count}
	if avg != nil {
		s.AverageRating = *avg
	}
	return s, nil
}
