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
}
