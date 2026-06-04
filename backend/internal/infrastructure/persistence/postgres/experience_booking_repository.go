package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// ExperienceBookingRepository is the Postgres implementation of
// experiencebooking.Repository (S81 — promotes the S80 memory stub to
// durable storage).
//
// Behaviour matches the memory adapter so swapping the backing store
// doesn't reshuffle paged views or change the overlap-check semantics
// the service relies on at Create time. The memory adapter remains
// available for the e2e test harness.
//
// The querier dependency lets the repo run either against the pool or
// inside a pgx.Tx — the UnitOfWork (S87) uses the tx-bound form so an
// ExperienceBooking write and its outbox event commit atomically.
type ExperienceBookingRepository struct {
	pool querier
}

// NewExperienceBookingRepository builds a repo. db may be the pool (for
// reads outside any UoW) or a pgx.Tx (so the UoW can atomically commit
// the booking write alongside the outbox event).
func NewExperienceBookingRepository(db querier) *ExperienceBookingRepository {
	return &ExperienceBookingRepository{pool: db}
}

var _ experiencebooking.Repository = (*ExperienceBookingRepository)(nil)

// experienceBookingColumns lists every column in the order the
// Scan/INSERT pairs expect — kept as a const so a column-rename touches
// one spot.
const experienceBookingColumns = `id, experience_id, host_id, guest_id,
	session_start_at, session_duration_minutes, guests,
	pricing_jsonb, status, created_at, updated_at`

// pricingDTO is the on-the-wire JSON shape stored in pricing_jsonb. We
// deliberately store cents+currency rather than the typed shared.Money
// (which is unexported-field) so the column survives a JSON round-trip
// independent of the Go struct's internal layout. The read-side
// reconstructs Money via shared.NewMoney(cents, currency).
type pricingDTO struct {
	PricePerGuestCents int64  `json:"pricePerGuestCents"`
	Currency           string `json:"currency"`
	Guests             int    `json:"guests"`
	SubtotalCents      int64  `json:"subtotalCents"`
	ServiceFeeCents    int64  `json:"serviceFeeCents"`
	TotalCents         int64  `json:"totalCents"`
}

func marshalPricing(p experiencebooking.Pricing) ([]byte, error) {
	dto := pricingDTO{
		PricePerGuestCents: p.PricePerGuest.AmountCents(),
		Currency:           p.PricePerGuest.Currency(),
		Guests:             p.Guests,
		SubtotalCents:      p.Subtotal.AmountCents(),
		ServiceFeeCents:    p.ServiceFee.AmountCents(),
		TotalCents:         p.Total.AmountCents(),
	}
	out, err := json.Marshal(dto)
	if err != nil {
		return nil, shared.NewValidationError("experiencebooking: pricing not json-encodable: " + err.Error())
	}
	return out, nil
}

func unmarshalPricing(raw []byte) (experiencebooking.Pricing, error) {
	var dto pricingDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return experiencebooking.Pricing{}, shared.NewValidationError("experiencebooking: pricing not json-decodable: " + err.Error())
	}
	perGuest, err := shared.NewMoney(dto.PricePerGuestCents, dto.Currency)
	if err != nil {
		return experiencebooking.Pricing{}, shared.NewValidationError("experiencebooking: stored pricePerGuest invalid: " + err.Error())
	}
	subtotal, err := shared.NewMoney(dto.SubtotalCents, dto.Currency)
	if err != nil {
		return experiencebooking.Pricing{}, shared.NewValidationError("experiencebooking: stored subtotal invalid: " + err.Error())
	}
	fee, err := shared.NewMoney(dto.ServiceFeeCents, dto.Currency)
	if err != nil {
		return experiencebooking.Pricing{}, shared.NewValidationError("experiencebooking: stored serviceFee invalid: " + err.Error())
	}
	total, err := shared.NewMoney(dto.TotalCents, dto.Currency)
	if err != nil {
		return experiencebooking.Pricing{}, shared.NewValidationError("experiencebooking: stored total invalid: " + err.Error())
	}
	return experiencebooking.Pricing{
		PricePerGuest: perGuest,
		Guests:        dto.Guests,
		Subtotal:      subtotal,
		ServiceFee:    fee,
		Total:         total,
	}, nil
}

func (r *ExperienceBookingRepository) Create(ctx context.Context, b *experiencebooking.Booking) error {
	pricing, err := marshalPricing(b.Pricing)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO experience_bookings (`+experienceBookingColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		b.ID, b.ExperienceID, b.HostID, b.GuestID,
		b.Session.StartAt, b.Session.DurationMinutes, b.Guests,
		pricing, string(b.Status), b.CreatedAt, b.UpdatedAt,
	)
	return mapError(err)
}

func (r *ExperienceBookingRepository) Update(ctx context.Context, b *experiencebooking.Booking) error {
	pricing, err := marshalPricing(b.Pricing)
	if err != nil {
		return err
	}
	// experience_id, host_id, guest_id and created_at are immutable
	// post-Create; everything else can shift under the domain's Confirm
	// / Cancel / Complete state transitions or a future re-price flow.
	tag, err := r.pool.Exec(ctx, `
		UPDATE experience_bookings SET
			session_start_at         = $2,
			session_duration_minutes = $3,
			guests                   = $4,
			pricing_jsonb            = $5,
			status                   = $6,
			updated_at               = $7
		WHERE id = $1`,
		b.ID, b.Session.StartAt, b.Session.DurationMinutes, b.Guests,
		pricing, string(b.Status), b.UpdatedAt,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *ExperienceBookingRepository) FindByID(ctx context.Context, id uuid.UUID) (*experiencebooking.Booking, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+experienceBookingColumns+` FROM experience_bookings WHERE id = $1`, id)
	return scanExperienceBooking(row)
}

func (r *ExperienceBookingRepository) ListByGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*experiencebooking.Booking], error) {
	return r.listBy(ctx, "guest_id", guestID, page)
}

func (r *ExperienceBookingRepository) ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*experiencebooking.Booking], error) {
	return r.listBy(ctx, "host_id", hostID, page)
}

// listBy is the shared paged query for ListByGuest / ListByHost. The
// column name is interpolated rather than parameterised because pgx
// can't $-bind identifiers — the caller passes a fixed string from a
// closed set.
func (r *ExperienceBookingRepository) listBy(ctx context.Context, col string, id uuid.UUID, page shared.Page) (shared.PageResult[*experiencebooking.Booking], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM experience_bookings WHERE `+col+` = $1`, id,
	).Scan(&total); err != nil {
		return shared.PageResult[*experiencebooking.Booking]{}, mapError(err)
	}

	// ORDER BY (created_at DESC, id ASC) matches the memory adapter so
	// swapping the backing store doesn't reshuffle the guest's "My
	// experiences" or host dashboard paged views.
	rows, err := r.pool.Query(ctx,
		`SELECT `+experienceBookingColumns+` FROM experience_bookings
		 WHERE `+col+` = $1
		 ORDER BY created_at DESC, id ASC
		 LIMIT $2 OFFSET $3`,
		id, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*experiencebooking.Booking]{}, mapError(err)
	}
	defer rows.Close()

	out, err := scanExperienceBookingRows(rows)
	if err != nil {
		return shared.PageResult[*experiencebooking.Booking]{}, err
	}
	return shared.PageResult[*experiencebooking.Booking]{Items: out, Total: total}, nil
}

// FindOverlapping returns every non-cancelled booking against the same
// experience whose session window overlaps the supplied [startAt, endAt)
// half-open range. Cancelled bookings are excluded so a re-book of the
// freed slot succeeds.
//
// Half-open overlap: two windows [a.start, a.end) and [b.start, b.end)
// overlap iff a.start < b.end AND b.start < a.end. Translated to SQL
// against the row (b) and the parameter window (a):
//
//	session_start_at < $endAt
//	AND $startAt < session_start_at + (session_duration_minutes * interval '1 minute')
//
// The first predicate hits idx_xb_overlap; the second is a row-level
// filter on the duration column (kept as expression-of-row so the
// planner does not need a functional index).
func (r *ExperienceBookingRepository) FindOverlapping(ctx context.Context, experienceID uuid.UUID, startAt, endAt time.Time) ([]*experiencebooking.Booking, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+experienceBookingColumns+` FROM experience_bookings
		 WHERE experience_id = $1
		   AND status <> 'cancelled'
		   AND session_start_at < $3
		   AND $2 < session_start_at + (session_duration_minutes * interval '1 minute')
		 ORDER BY session_start_at ASC, id ASC`,
		experienceID, startAt, endAt,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanExperienceBookingRows(rows)
}

// FindConfirmedPastSession returns confirmed bookings whose session
// window has already closed by `before`. The scheduler (S82) calls this
// every 5 minutes to flip them to completed. Limit ≤0 means no cap.
// Ordered oldest-end-first so the scheduler drains backlog
// deterministically.
func (r *ExperienceBookingRepository) FindConfirmedPastSession(ctx context.Context, before time.Time, limit int) ([]*experiencebooking.Booking, error) {
	query := `SELECT ` + experienceBookingColumns + ` FROM experience_bookings
	          WHERE status = 'confirmed'
	            AND session_start_at + (session_duration_minutes * interval '1 minute') <= $1
	          ORDER BY session_start_at ASC, id ASC`
	args := []any{before}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanExperienceBookingRows(rows)
}

func scanExperienceBooking(s pgxScanner) (*experiencebooking.Booking, error) {
	var (
		b           experiencebooking.Booking
		status      string
		pricingJSON []byte
	)
	if err := s.Scan(
		&b.ID, &b.ExperienceID, &b.HostID, &b.GuestID,
		&b.Session.StartAt, &b.Session.DurationMinutes, &b.Guests,
		&pricingJSON, &status, &b.CreatedAt, &b.UpdatedAt,
	); err != nil {
		return nil, mapError(err)
	}
	b.Status = experiencebooking.Status(status)
	// Normalise StartAt to UTC — the domain's NewSession constructor
	// stores UTC and behaviour-equivalent reads should not surface a
	// driver-local zone offset (pgx tends to attach the server tz).
	b.Session.StartAt = b.Session.StartAt.UTC()
	b.CreatedAt = b.CreatedAt.UTC()
	b.UpdatedAt = b.UpdatedAt.UTC()
	pricing, err := unmarshalPricing(pricingJSON)
	if err != nil {
		return nil, err
	}
	b.Pricing = pricing
	return &b, nil
}

func scanExperienceBookingRows(rows rowsScanner) ([]*experiencebooking.Booking, error) {
	out := make([]*experiencebooking.Booking, 0)
	for rows.Next() {
		b, err := scanExperienceBooking(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return out, nil
}
