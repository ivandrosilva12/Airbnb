package houserules

import (
	"context"

	"github.com/google/uuid"
)

// Repository persists Rules versions and Acceptance records. Rules
// are append-only by Version (per (propertyID, version) PK); the only
// "delete" path is replacing the current head with a new bump.
//
// Acceptances are append-only with idempotent re-insert on the same
// bookingID (S47: a retry of the booking flow must NOT produce two
// acceptance rows for the same booking).
type Repository interface {
	// GetCurrent returns the most-recent Rules version for a property,
	// or ErrNotFound when the host has never authored rules.
	GetCurrent(ctx context.Context, propertyID uuid.UUID) (*Rules, error)
	// GetVersion returns a specific past version. Required so an
	// Acceptance lookup can render exactly the text the guest saw.
	GetVersion(ctx context.Context, propertyID uuid.UUID, version int) (*Rules, error)
	// Save persists a new Rules row. The expected pattern is: read
	// current, call Bump (or New if missing), Save the result. Returns
	// shared.ErrConflict if the version already exists — a stricter
	// guard than a blind upsert, so a concurrent edit fails loud
	// rather than silently overwriting the other writer.
	Save(ctx context.Context, r *Rules) error

	// RecordAcceptance persists an Acceptance. Idempotent on bookingID:
	// a second call with the same bookingID returns nil without
	// overwriting the first row.
	RecordAcceptance(ctx context.Context, a *Acceptance) error
	// AcceptanceFor returns the acceptance row for a booking, or
	// ErrNotFound when no acceptance was recorded (host had no rules,
	// or the post-commit recording failed and was not retried).
	AcceptanceFor(ctx context.Context, bookingID uuid.UUID) (*Acceptance, error)
}
