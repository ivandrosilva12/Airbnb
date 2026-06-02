package houserules

import (
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Acceptance is the per-booking proof that a guest agreed to a
// specific Version of a property's house rules at a specific time.
// It is the legal/dispute artefact: the host can later answer "did
// the guest acknowledge the no-pets rule?" by joining the booking to
// its Acceptance and looking up rules at AcceptedVersion.
//
// One Acceptance exists per (BookingID): a guest cannot re-accept,
// and re-attempts during retry are idempotent at the repo layer.
type Acceptance struct {
	BookingID       uuid.UUID
	GuestID         uuid.UUID
	PropertyID      uuid.UUID
	AcceptedVersion int
	AcceptedAt      time.Time
}

// NewAcceptance constructs an Acceptance, validating its required
// fields. Returns shared.NewValidationError on missing inputs so the
// service surfaces 422 rather than persisting a junk row.
func NewAcceptance(bookingID, guestID, propertyID uuid.UUID, version int) (*Acceptance, error) {
	if bookingID == uuid.Nil {
		return nil, shared.NewValidationError("houserules: bookingID is required")
	}
	if guestID == uuid.Nil {
		return nil, shared.NewValidationError("houserules: guestID is required")
	}
	if propertyID == uuid.Nil {
		return nil, shared.NewValidationError("houserules: propertyID is required")
	}
	if version <= 0 {
		return nil, shared.NewValidationError("houserules: acceptedVersion must be > 0")
	}
	return &Acceptance{
		BookingID:       bookingID,
		GuestID:         guestID,
		PropertyID:      propertyID,
		AcceptedVersion: version,
		AcceptedAt:      time.Now().UTC(),
	}, nil
}
