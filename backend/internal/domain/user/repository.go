package user

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the User aggregate. Implementations
// live in the infrastructure layer.
type Repository interface {
	Create(ctx context.Context, u *User) error
	Update(ctx context.Context, u *User) error
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	FindByKeycloakSub(ctx context.Context, sub string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	// FindByPayoutAccountID resolves the host that owns a connected payout
	// account (a Stripe acct_…), used to reconcile Connect webhooks.
	FindByPayoutAccountID(ctx context.Context, accountID string) (*User, error)
}
