// Package couponapp contains promo-code use cases: admin management of coupons
// and the read-only computation a guest needs to preview a code's discount.
package couponapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/coupon"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates coupon use cases.
type Service struct {
	coupons coupon.Repository
}

// NewService wires the coupon application service.
func NewService(coupons coupon.Repository) *Service {
	return &Service{coupons: coupons}
}

// CreateInput carries the data to mint a coupon. Kind selects how the discount
// is expressed: "percentage" uses Percent (a fraction in (0,1]); "fixed" uses
// AmountCents in Currency.
type CreateInput struct {
	Code           string
	Kind           string
	Percent        float64
	AmountCents    int64
	Currency       string
	MinNights      int
	MaxRedemptions int
	ExpiresAt      *time.Time
}

// Create mints a coupon (admin only — authorization is enforced by the route).
func (s *Service) Create(ctx context.Context, in CreateInput) (*coupon.Coupon, error) {
	var (
		c   *coupon.Coupon
		err error
	)
	switch coupon.Kind(in.Kind) {
	case coupon.KindPercentage:
		c, err = coupon.NewPercentage(in.Code, in.Percent, in.MinNights, in.MaxRedemptions, in.ExpiresAt)
	case coupon.KindFixed:
		c, err = coupon.NewFixed(in.Code, in.AmountCents, in.Currency, in.MinNights, in.MaxRedemptions, in.ExpiresAt)
	default:
		return nil, shared.NewValidationError("coupon kind must be 'percentage' or 'fixed'")
	}
	if err != nil {
		return nil, err
	}
	if err := s.coupons.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// List returns coupons, most recent first.
func (s *Service) List(ctx context.Context, page shared.Page) (shared.PageResult[*coupon.Coupon], error) {
	return s.coupons.List(ctx, page)
}

// Deactivate disables a coupon so it can no longer be applied.
func (s *Service) Deactivate(ctx context.Context, id uuid.UUID) (*coupon.Coupon, error) {
	c, err := s.coupons.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Deactivate()
	if err := s.coupons.Update(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}
