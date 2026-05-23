// Package block is the bounded context for host-managed calendar blocks: date
// ranges a host marks unavailable on a listing (e.g. maintenance or personal
// use), independent of guest bookings.
package block

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Block is the aggregate root for a blocked date range on a property. It reuses
// the booking DateRange so calendar/overlap semantics stay consistent.
type Block struct {
	ID         uuid.UUID
	PropertyID uuid.UUID
	Dates      booking.DateRange
	Reason     string
	CreatedAt  time.Time
}

// New creates a Block, enforcing a reasonable reason length.
func New(propertyID uuid.UUID, dates booking.DateRange, reason string) (*Block, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) > 200 {
		return nil, shared.NewValidationError("reason is too long")
	}
	return &Block{
		ID:         uuid.New(),
		PropertyID: propertyID,
		Dates:      dates,
		Reason:     reason,
		CreatedAt:  time.Now().UTC(),
	}, nil
}
