// Package searchapp is a read-model query service that composes the property,
// booking and block contexts to offer date-aware listing search: when a date
// window is supplied, listings that are booked or host-blocked for that window
// are excluded.
package searchapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/block"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates listing search.
type Service struct {
	properties property.Repository
	bookings   booking.Repository
	blocks     block.Repository
}

// NewService wires the search query service.
func NewService(properties property.Repository, bookings booking.Repository, blocks block.Repository) *Service {
	return &Service{properties: properties, bookings: bookings, blocks: blocks}
}

// DateWindow is an optional availability filter for search.
type DateWindow struct {
	CheckIn  time.Time
	CheckOut time.Time
}

// Search runs a filtered listing search. When window is non-nil, listings that
// are booked or host-blocked over the window are excluded.
func (s *Service) Search(ctx context.Context, criteria property.SearchCriteria, window *DateWindow) (shared.PageResult[*property.Property], error) {
	if window != nil {
		if !window.CheckOut.After(window.CheckIn) {
			return shared.PageResult[*property.Property]{}, shared.NewValidationError("checkOut must be after checkIn")
		}
		booked, err := s.bookings.BookedPropertyIDs(ctx, window.CheckIn, window.CheckOut)
		if err != nil {
			return shared.PageResult[*property.Property]{}, err
		}
		blocked, err := s.blocks.BlockedPropertyIDs(ctx, window.CheckIn, window.CheckOut)
		if err != nil {
			return shared.PageResult[*property.Property]{}, err
		}
		criteria.ExcludeIDs = dedupeIDs(append(booked, blocked...))
	}
	return s.properties.Search(ctx, criteria)
}

func dedupeIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := ids[:0]
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
