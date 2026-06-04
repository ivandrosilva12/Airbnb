package audit

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Filter narrows a query against the audit trail. All fields are
// optional; nil/zero means "match all". The repository ANDs the
// non-zero fields together.
type Filter struct {
	ActorID    uuid.UUID  // exact match
	Action     Action     // exact match
	TargetType TargetType // exact match
	TargetID   uuid.UUID  // exact match
}

// Repository is the persistence port. Writes are append-only; reads
// support compliance + forensics queries (page through history,
// filter by actor/target).
type Repository interface {
	// Create appends an event to the trail. Errors propagate; the
	// service layer treats audit-record failure as a hard error on the
	// success path of the originating admin action (we'd rather fail
	// the admin operation than persist a change without a trail).
	Create(ctx context.Context, e *Event) error

	// List returns events matching f, newest first, paginated. Total
	// count is included so the admin UI can show "Page N of M".
	List(ctx context.Context, f Filter, page shared.Page) (shared.PageResult[*Event], error)

	// AnonymizeBySubject scrubs links to the given user across the audit
	// trail — the GDPR right-to-erasure path. Where the user appears as
	// actor_id or as a user-target target_id, the column is reset to the
	// zero UUID; the event row itself is RETAINED because the audit
	// trail is the platform's compliance evidence (regulators expect to
	// see "user X was suspended on day Y" even after X erases their
	// account; the row stands, the personal link is severed). Returns
	// the number of rows touched.
	AnonymizeBySubject(ctx context.Context, userID uuid.UUID) (int, error)
}
