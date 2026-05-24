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

const reviewColumns = `id, booking_id, property_id, author_id, guest_id, kind, rating, comment, created_at`

func (r *ReviewRepository) Create(ctx context.Context, rv *review.Review) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO reviews (`+reviewColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		rv.ID, rv.BookingID, rv.PropertyID, rv.AuthorID, rv.GuestID, string(rv.Kind), rv.Rating, rv.Comment, rv.CreatedAt,
	)
	return mapError(err)
}

func (r *ReviewRepository) ExistsForBookingKind(ctx context.Context, bookingID uuid.UUID, kind review.Kind) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM reviews WHERE booking_id=$1 AND kind=$2)`, bookingID, string(kind),
	).Scan(&exists)
	return exists, mapError(err)
}

func (r *ReviewRepository) ListByProperty(ctx context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*review.Review], error) {
	return r.list(ctx, `property_id=$1 AND kind='guest_to_property'`, propertyID, page)
}

func (r *ReviewRepository) ListAboutGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*review.Review], error) {
	return r.list(ctx, `guest_id=$1 AND kind='host_to_guest'`, guestID, page)
}

func (r *ReviewRepository) list(ctx context.Context, where string, arg any, page shared.Page) (shared.PageResult[*review.Review], error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM reviews WHERE `+where, arg).Scan(&total); err != nil {
		return shared.PageResult[*review.Review]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+reviewColumns+` FROM reviews WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		arg, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*review.Review]{}, mapError(err)
	}
	defer rows.Close()

	var items []*review.Review
	for rows.Next() {
		rv, err := scanReview(rows)
		if err != nil {
			return shared.PageResult[*review.Review]{}, mapError(err)
		}
		items = append(items, rv)
	}
	return shared.PageResult[*review.Review]{Items: items, Total: total}, mapError(rows.Err())
}

func (r *ReviewRepository) SummaryForProperty(ctx context.Context, propertyID uuid.UUID) (review.Summary, error) {
	return r.summary(ctx, `property_id=$1 AND kind='guest_to_property'`, propertyID)
}

func (r *ReviewRepository) SummaryForGuest(ctx context.Context, guestID uuid.UUID) (review.Summary, error) {
	return r.summary(ctx, `guest_id=$1 AND kind='host_to_guest'`, guestID)
}

func (r *ReviewRepository) summary(ctx context.Context, where string, subjectID uuid.UUID) (review.Summary, error) {
	var (
		avg   *float64
		count int64
	)
	err := r.pool.QueryRow(ctx,
		`SELECT AVG(rating), COUNT(*) FROM reviews WHERE `+where, subjectID,
	).Scan(&avg, &count)
	if err != nil {
		return review.Summary{}, mapError(err)
	}
	s := review.Summary{SubjectID: subjectID, Count: count}
	if avg != nil {
		s.AverageRating = *avg
	}
	return s, nil
}

func scanReview(row rowScanner) (*review.Review, error) {
	var (
		rv   review.Review
		kind string
	)
	err := row.Scan(&rv.ID, &rv.BookingID, &rv.PropertyID, &rv.AuthorID, &rv.GuestID, &kind, &rv.Rating, &rv.Comment, &rv.CreatedAt)
	if err != nil {
		return nil, err
	}
	rv.Kind = review.Kind(kind)
	return &rv, nil
}
