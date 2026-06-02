package user

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// ListFilter narrows the admin user list (S65). All fields AND together; an
// unset field is a "no filter" wildcard. EmailContains is a substring match
// (case-insensitive) so an operator can find a user by partial email — the
// support workflow we built S61 to power.
type ListFilter struct {
	EmailContains string
	Role          Role // empty = any role
	ActiveOnly    bool // when true, hides suspended accounts
}

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
	// List returns users matching filter, paginated, newest first. Used by
	// the admin user-management page (S65); no other surface lists users.
	List(ctx context.Context, filter ListFilter, page shared.Page) (shared.PageResult[*User], error)
}
