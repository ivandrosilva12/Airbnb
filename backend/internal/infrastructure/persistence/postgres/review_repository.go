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

const reviewColumns = `id, booking_id, property_id, author_id, guest_id, kind, rating, comment, response, responded_at, created_at, ` +
	`rating_cleanliness, rating_accuracy, rating_communication, rating_location, rating_checkin, rating_value`

func (r *ReviewRepository) Create(ctx context.Context, rv *review.Review) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO reviews (`+reviewColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		rv.ID, rv.BookingID, rv.PropertyID, rv.AuthorID, rv.GuestID, string(rv.Kind), rv.Rating, rv.Comment,
		rv.Response, rv.RespondedAt, rv.CreatedAt,
		rv.Categories.Cleanliness, rv.Categories.Accuracy, rv.Categories.Communication,
		rv.Categories.Location, rv.Categories.CheckIn, rv.Categories.Value,
	)
	return mapError(err)
}

func (r *ReviewRepository) FindByID(ctx context.Context, id uuid.UUID) (*review.Review, error) {
	rv, err := scanReview(r.pool.QueryRow(ctx, `SELECT `+reviewColumns+` FROM reviews WHERE id=$1`, id))
	if err != nil {
		return nil, mapError(err)
	}
	return rv, nil
}

func (r *ReviewRepository) Update(ctx context.Context, rv *review.Review) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE reviews SET comment=$2, response=$3, responded_at=$4 WHERE id=$1`,
		rv.ID, rv.Comment, rv.Response, rv.RespondedAt,
	)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
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

func (r *ReviewRepository) AnonymizeByAuthor(ctx context.Context, authorID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE reviews SET comment='' WHERE author_id=$1`, authorID)
	return mapError(err)
}

func (r *ReviewRepository) summary(ctx context.Context, where string, subjectID uuid.UUID) (review.Summary, error) {
	var (
		avg                    *float64
		count                  int64
		cl, ac, co, lo, ci, va *float64
	)
	err := r.pool.QueryRow(ctx,
		`SELECT AVG(rating), COUNT(*),
		        AVG(NULLIF(rating_cleanliness,0)),
		        AVG(NULLIF(rating_accuracy,0)),
		        AVG(NULLIF(rating_communication,0)),
		        AVG(NULLIF(rating_location,0)),
		        AVG(NULLIF(rating_checkin,0)),
		        AVG(NULLIF(rating_value,0))
		   FROM reviews WHERE `+where, subjectID,
	).Scan(&avg, &count, &cl, &ac, &co, &lo, &ci, &va)
	if err != nil {
		return review.Summary{}, mapError(err)
	}
	s := review.Summary{SubjectID: subjectID, Count: count}
	if avg != nil {
		s.AverageRating = *avg
	}
	deref := func(p *float64) float64 {
		if p != nil {
			return *p
		}
		return 0
	}
	s.Categories = review.CategoryAverages{
		Cleanliness:   deref(cl),
		Accuracy:      deref(ac),
		Communication: deref(co),
		Location:      deref(lo),
		CheckIn:       deref(ci),
		Value:         deref(va),
	}
	return s, nil
}

func scanReview(row rowScanner) (*review.Review, error) {
	var (
		rv   review.Review
		kind string
	)
	err := row.Scan(&rv.ID, &rv.BookingID, &rv.PropertyID, &rv.AuthorID, &rv.GuestID, &kind, &rv.Rating, &rv.Comment, &rv.Response, &rv.RespondedAt, &rv.CreatedAt,
		&rv.Categories.Cleanliness, &rv.Categories.Accuracy, &rv.Categories.Communication, &rv.Categories.Location, &rv.Categories.CheckIn, &rv.Categories.Value)
	if err != nil {
		return nil, err
	}
	rv.Kind = review.Kind(kind)
	return &rv, nil
}
