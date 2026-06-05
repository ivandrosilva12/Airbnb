// Package blockapp contains host calendar-block use cases: a host blocks/unblocks
// date ranges on a listing they own.
package blockapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/block"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// CohostAuthorizer is the relaxed permission gate used to admit a co-host
// with PermManageCalendar on a listing they don't own. The block service
// defers to it when set; otherwise it falls back to strict ownership.
type CohostAuthorizer interface {
	Authorize(ctx context.Context, actorID, propertyID uuid.UUID, required property.CohostPermission) (*property.Property, error)
}

// Service orchestrates block use cases.
type Service struct {
	blocks     block.Repository
	properties property.Repository
	bookings   booking.Repository
	cohosts    CohostAuthorizer
}

// NewService wires the block application service.
func NewService(blocks block.Repository, properties property.Repository) *Service {
	return &Service{blocks: blocks, properties: properties}
}

// WithCohosts plugs in the relaxed gate so co-hosts with manage_calendar
// can mutate blocks. Returns the receiver for chaining.
func (s *Service) WithCohosts(c CohostAuthorizer) *Service {
	s.cohosts = c
	return s
}

// WithBookings plugs in the bookings repo so Import can flag ranges that
// collide with confirmed reservations (S168). Optional — when omitted the
// import only de-dupes against existing blocks (pre-S168 behaviour).
func (s *Service) WithBookings(b booking.Repository) *Service {
	s.bookings = b
	return s
}

// access resolves the calling user to the listing they're acting on, honouring
// the co-host gate when configured. The strict path (ownership only) is used
// when no authorizer was wired — preserves the pre-S17 behaviour.
func (s *Service) access(ctx context.Context, actorID, propertyID uuid.UUID) (*property.Property, error) {
	if s.cohosts != nil {
		return s.cohosts.Authorize(ctx, actorID, propertyID, property.PermManageCalendar)
	}
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	return prop, nil
}

// Create blocks a date range on a listing the host owns (or a co-host with
// the manage_calendar permission may act on the primary host's behalf).
func (s *Service) Create(ctx context.Context, hostID, propertyID uuid.UUID, from, to time.Time, reason string) (*block.Block, error) {
	if _, err := s.access(ctx, hostID, propertyID); err != nil {
		return nil, err
	}
	dates, err := booking.NewDateRange(from, to)
	if err != nil {
		return nil, err
	}
	overlap, err := s.blocks.HasOverlap(ctx, propertyID, dates)
	if err != nil {
		return nil, err
	}
	if overlap {
		return nil, shared.NewValidationError("those dates are already blocked")
	}
	b, err := block.New(propertyID, dates, reason)
	if err != nil {
		return nil, err
	}
	if err := s.blocks.Create(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// ImportRange is a busy range to import as a host block (e.g. from an external
// iCal feed).
type ImportRange struct {
	From   time.Time
	To     time.Time
	Reason string
}

// DateRange is a plain [from, to) window surfaced in ImportResult so callers
// can render conflicts back to the host without dragging the booking
// aggregate into the HTTP layer. Dates are truncated to the day in UTC.
type DateRange struct {
	From time.Time
	To   time.Time
}

// ImportResult is the rich outcome of an iCal import: how many blocks
// landed, how many were dropped as duplicates of existing blocks, and the
// specific ranges that collided with confirmed reservations. Surfacing
// SkippedBookingConflict lets the host's import UI say "8 imported,
// 2 conflicts: 2026-08-10..15 already booked" instead of silently dropping
// the dates.
type ImportResult struct {
	Created                int
	SkippedBlockOverlap    int
	SkippedBookingConflict []DateRange
}

// Import creates blocks for the given busy ranges on a listing the caller may
// manage (primary host or co-host with manage_calendar). Skips ranges in the
// past or ones that already overlap an existing block (so re-importing the
// same feed is idempotent), and — when a bookings repo is wired via
// WithBookings — surfaces ranges that collide with a confirmed reservation
// rather than silently dropping them (S168).
func (s *Service) Import(ctx context.Context, hostID, propertyID uuid.UUID, ranges []ImportRange) (ImportResult, error) {
	var result ImportResult
	if _, err := s.access(ctx, hostID, propertyID); err != nil {
		return result, err
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for _, r := range ranges {
		if !r.To.After(today) {
			continue // wholly in the past
		}
		from := r.From
		if from.Before(today) {
			from = today // clamp an in-progress range to its remaining future nights
		}
		dates, err := booking.NewDateRange(from, r.To)
		if err != nil {
			continue // skip malformed ranges rather than failing the whole import
		}
		overlap, err := s.blocks.HasOverlap(ctx, propertyID, dates)
		if err != nil {
			return result, err
		}
		if overlap {
			result.SkippedBlockOverlap++
			continue
		}
		if s.bookings != nil {
			conflict, err := s.bookings.HasConfirmedOverlap(ctx, propertyID, dates)
			if err != nil {
				return result, err
			}
			if conflict {
				result.SkippedBookingConflict = append(result.SkippedBookingConflict, DateRange{
					From: dates.CheckIn,
					To:   dates.CheckOut,
				})
				continue
			}
		}
		b, err := block.New(propertyID, dates, r.Reason)
		if err != nil {
			continue
		}
		if err := s.blocks.Create(ctx, b); err != nil {
			return result, err
		}
		result.Created++
	}
	return result, nil
}

// ListForHost returns the blocks on a listing the caller may manage (primary
// host or co-host with manage_calendar).
func (s *Service) ListForHost(ctx context.Context, hostID, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*block.Block], error) {
	if _, err := s.access(ctx, hostID, propertyID); err != nil {
		return shared.PageResult[*block.Block]{}, err
	}
	return s.blocks.ListByProperty(ctx, propertyID, page)
}

// Delete removes a block on a listing the caller may manage (primary host or
// co-host with manage_calendar). The block id is resolved first so the access
// check uses the actual property the block belongs to.
func (s *Service) Delete(ctx context.Context, hostID, blockID uuid.UUID) error {
	b, err := s.blocks.FindByID(ctx, blockID)
	if err != nil {
		return err
	}
	if _, err := s.access(ctx, hostID, b.PropertyID); err != nil {
		return err
	}
	return s.blocks.Delete(ctx, blockID)
}
