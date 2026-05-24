package report

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the Report aggregate.
type Repository interface {
	Create(ctx context.Context, r *Report) error
	Update(ctx context.Context, r *Report) error
	FindByID(ctx context.Context, id uuid.UUID) (*Report, error)
	// ListByStatus returns reports in the given status, newest first — used to
	// drive the administrator moderation queue.
	ListByStatus(ctx context.Context, status Status, page shared.Page) (shared.PageResult[*Report], error)
	// HasOpenFromReporter reports whether the reporter already has an open report
	// on the listing, so a user cannot pile duplicates onto the queue.
	HasOpenFromReporter(ctx context.Context, propertyID, reporterID uuid.UUID) (bool, error)
}
