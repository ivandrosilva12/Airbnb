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

// Service orchestrates block use cases.
type Service struct {
	blocks     block.Repository
	properties property.Repository
}

// NewService wires the block application service.
func NewService(blocks block.Repository, properties property.Repository) *Service {
	return &Service{blocks: blocks, properties: properties}
}

// Create blocks a date range on a listing the host owns.
func (s *Service) Create(ctx context.Context, hostID, propertyID uuid.UUID, from, to time.Time, reason string) (*block.Block, error) {
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(hostID) {
		return nil, shared.ErrForbidden
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

// ListForHost returns the blocks on a listing the host owns.
func (s *Service) ListForHost(ctx context.Context, hostID, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*block.Block], error) {
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return shared.PageResult[*block.Block]{}, err
	}
	if !prop.IsOwnedBy(hostID) {
		return shared.PageResult[*block.Block]{}, shared.ErrForbidden
	}
	return s.blocks.ListByProperty(ctx, propertyID, page)
}

// Delete removes a block on a listing the host owns.
func (s *Service) Delete(ctx context.Context, hostID, blockID uuid.UUID) error {
	b, err := s.blocks.FindByID(ctx, blockID)
	if err != nil {
		return err
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return err
	}
	if !prop.IsOwnedBy(hostID) {
		return shared.ErrForbidden
	}
	return s.blocks.Delete(ctx, blockID)
}
