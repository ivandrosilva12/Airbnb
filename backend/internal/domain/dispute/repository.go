package dispute

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the dispute context.
type Repository interface {
	// Save persists a Dispute aggregate (and its Evidence collection). The
	// implementation should write atomically and reject duplicate active
	// disputes for the same booking (conflict).
	Save(ctx context.Context, d *Dispute) error
	// FindByID returns the dispute or shared.ErrNotFound.
	FindByID(ctx context.Context, id uuid.UUID) (*Dispute, error)
	// FindActiveByBooking returns the single open / under_review dispute for a
	// booking, or shared.ErrNotFound. Used to enforce the "one active dispute
	// per booking" rule at the application layer.
	FindActiveByBooking(ctx context.Context, bookingID uuid.UUID) (*Dispute, error)
	// ListByOpener returns disputes opened by a user (most recent first).
	ListByOpener(ctx context.Context, openerID uuid.UUID, page shared.Page) (shared.PageResult[*Dispute], error)
	// ListByBookings returns disputes attached to any of the given bookings —
	// used to surface disputes a host needs to respond to (where the host is
	// not the opener but owns the listing).
	ListByBookings(ctx context.Context, bookingIDs []uuid.UUID, page shared.Page) (shared.PageResult[*Dispute], error)
	// ListOpen returns the moderation queue (open + under_review), oldest first.
	ListOpen(ctx context.Context, page shared.Page) (shared.PageResult[*Dispute], error)
	// AnonymizeByUser scrubs PII from every dispute the user touched (as
	// opener or evidence contributor) — the free-text reason / host
	// response / resolution and any evidence notes they authored. The
	// dispute row itself is RETAINED because it is a legal artefact tied
	// to the booking: the counterparty has a legitimate interest in the
	// claim record and refunds/damage debits already moved money on its
	// authority. The opener_id FK stays valid because the user row is
	// kept (anonymised) — the row becomes "Deleted user".
	// Returns the number of dispute rows touched.
	AnonymizeByUser(ctx context.Context, userID uuid.UUID) (int, error)
}
