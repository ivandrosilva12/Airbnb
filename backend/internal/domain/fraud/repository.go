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
	// AnonymizeByGuest scrubs the guest_id column on every assessment
	// the user triggered — the GDPR right-to-erasure path. The
	// assessment row is RETAINED because it is a forensic record tied
	// to the booking_id (the platform's defence if a chargeback or
	// regulator query asks "did you flag this booking before
	// confirming"). After erase the guest_id column holds the zero
	// UUID so the row no longer ties back to a personal account.
	// Returns the number of rows touched.
	AnonymizeByGuest(ctx context.Context, guestID uuid.UUID) (int, error)
}
