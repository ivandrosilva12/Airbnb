package splitpayment

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the SplitPayment aggregate. Shares
// are persisted as part of the aggregate (Create/Update write both rows
// atomically); implementations should not expose share-level CRUD.
type Repository interface {
	Create(ctx context.Context, sp *SplitPayment) error
	Update(ctx context.Context, sp *SplitPayment) error
	FindByID(ctx context.Context, id uuid.UUID) (*SplitPayment, error)
	// FindByBookingID returns the split attached to a booking, or
	// shared.ErrNotFound if the booking has no split.
	FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*SplitPayment, error)
	// ListForUser returns the splits where the user is either the organizer
	// or a share payer (matched by email). Newest first.
	ListForUser(ctx context.Context, userID uuid.UUID, email string) ([]*SplitPayment, error)
	// AnonymizeByUser scrubs PII from every split-payment record the user
	// touched: on shares where they were the invited payer (matched by
	// email, case-folded) the email is blanked. Organizer-side splits are
	// retained as-is — the organizer_id FK still resolves to the
	// (anonymised) user row, and the split is co-owned by the other
	// payers who have a legitimate interest in the record. Returns the
	// number of share rows scrubbed.
	AnonymizeByUser(ctx context.Context, userID uuid.UUID, email string) (int, error)
}
