package fraud

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// ListFilter narrows the admin assessment list. All fields are
// optional; the zero filter returns every assessment in the
// repository (paged).
type ListFilter struct {
	// MinLevel, when non-empty, restricts to assessments of at
	// least this severity. Useful for "show me everything high".
	MinLevel Level
}

// Repository is the persistence port for fraud Assessments.
// Save is the only write — assessments are immutable forensic
// records. Reads support both admin browsing (List) and a per-
// booking lookup (FindByBookingID) used by the admin booking
// detail view to surface "why did this fire".
type Repository interface {
	Save(ctx context.Context, a *Assessment) error
	FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*Assessment, error)
	List(ctx context.Context, f ListFilter, page shared.Page) (shared.PageResult[*Assessment], error)
}
