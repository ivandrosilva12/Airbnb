package coupon

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for coupons.
type Repository interface {
	Create(ctx context.Context, c *Coupon) error
	Update(ctx context.Context, c *Coupon) error
	FindByID(ctx context.Context, id uuid.UUID) (*Coupon, error)
	// FindByCode looks a coupon up by its (case-insensitive) code.
	FindByCode(ctx context.Context, code string) (*Coupon, error)
	List(ctx context.Context, page shared.Page) (shared.PageResult[*Coupon], error)
}
