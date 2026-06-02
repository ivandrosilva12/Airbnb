package messagetemplate

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the Template aggregate.
type Repository interface {
	Create(ctx context.Context, t *Template) error
	Update(ctx context.Context, t *Template) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindByID(ctx context.Context, id uuid.UUID) (*Template, error)
	// ListByHost returns the host's templates ordered by label ASC. The set
	// is intentionally unpaginated — a host's playbook is small by design.
	ListByHost(ctx context.Context, hostID uuid.UUID) ([]*Template, error)
}
