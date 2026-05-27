package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/offer"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OfferRepository is the Postgres implementation of offer.Repository.
type OfferRepository struct {
	pool *pgxpool.Pool
}

// NewOfferRepository builds an OfferRepository.
func NewOfferRepository(pool *pgxpool.Pool) *OfferRepository {
	return &OfferRepository{pool: pool}
}

var _ offer.Repository = (*OfferRepository)(nil)

const offerColumns = `id, property_id, host_id, guest_id, check_in, check_out, guests,
	price_cents, currency, message, kind, status, created_at, expires_at`

func (r *OfferRepository) Create(ctx context.Context, o *offer.Offer) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO offers (`+offerColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		o.ID, o.PropertyID, o.HostID, o.GuestID, o.CheckIn, o.CheckOut, o.Guests,
		o.PriceCents, o.Currency, o.Message, string(o.Kind), string(o.Status), o.CreatedAt, o.ExpiresAt,
	)
	return mapError(err)
}

func (r *OfferRepository) Update(ctx context.Context, o *offer.Offer) error {
	ct, err := r.pool.Exec(ctx, `UPDATE offers SET status=$2 WHERE id=$1`, o.ID, string(o.Status))
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *OfferRepository) FindByID(ctx context.Context, id uuid.UUID) (*offer.Offer, error) {
	return scanOffer(r.pool.QueryRow(ctx, `SELECT `+offerColumns+` FROM offers WHERE id=$1`, id))
}

func (r *OfferRepository) ListForGuest(ctx context.Context, guestID uuid.UUID) ([]*offer.Offer, error) {
	return r.list(ctx, `guest_id=$1`, guestID)
}

func (r *OfferRepository) ListForHost(ctx context.Context, hostID uuid.UUID) ([]*offer.Offer, error) {
	return r.list(ctx, `host_id=$1`, hostID)
}

func (r *OfferRepository) list(ctx context.Context, where string, arg uuid.UUID) ([]*offer.Offer, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+offerColumns+` FROM offers WHERE `+where+` ORDER BY created_at DESC`, arg)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []*offer.Offer
	for rows.Next() {
		o, err := scanOffer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, mapError(rows.Err())
}

func scanOffer(row rowScanner) (*offer.Offer, error) {
	var (
		o          offer.Offer
		kind, stat string
	)
	if err := row.Scan(&o.ID, &o.PropertyID, &o.HostID, &o.GuestID, &o.CheckIn, &o.CheckOut, &o.Guests,
		&o.PriceCents, &o.Currency, &o.Message, &kind, &stat, &o.CreatedAt, &o.ExpiresAt); err != nil {
		return nil, mapError(err)
	}
	o.Kind = offer.Kind(kind)
	o.Status = offer.Status(stat)
	return &o, nil
}
