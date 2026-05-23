// Package searchapp is a read-model query service that composes the property
// and booking contexts to offer date-aware listing search: when a date window
// is supplied, listings that are already booked for that window are excluded.
package searchapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
)

// Service orchestrates listing search.
type Service struct {
	properties property.Repository
	bookings   booking.Repository
}

// NewService wires the search query service.
func NewService(properties property.Repository, bookings booking.Repository) *Service {
	return &Service{properties: properties, bookings: bookings}
}

// DateWindow is an optional availability filter for search.
type DateWindow struct {
	CheckIn  time.Time
	CheckOut time.Time
}

// Search runs a filtered listing search. When window is non-nil, listings with
// an active booking overlapping the window are excluded.
func (s *Service) Search(ctx context.Context, criteria property.SearchCriteria, window *DateWindow) (shared.PageResult[*property.Property], error) {
	if window != nil {
		if !window.CheckOut.After(window.CheckIn) {
			return shared.PageResult[*property.Property]{}, shared.NewValidationError("checkOut must be after checkIn")
		}
		booked, err := s.bookings.BookedPropertyIDs(ctx, window.CheckIn, window.CheckOut)
		if err != nil {
			return shared.PageResult[*property.Property]{}, err
		}
		criteria.ExcludeIDs = booked
	}
	return s.properties.Search(ctx, criteria)
}
