// Package analyticsapp provides read-model reporting that composes several
// bounded contexts. It performs no writes — it aggregates listings, bookings
// and payments into figures a host cares about.
package analyticsapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// reportPage fetches a generous slice; hosts in this app have modest catalogues.
var reportPage = shared.Page{Limit: 1000, Offset: 0}

// HostMetrics is the read-model returned to a host.
type HostMetrics struct {
	Listings         int
	Published        int
	Bookings         int
	Pending          int
	Confirmed        int
	Completed        int
	Cancelled        int
	UpcomingCheckins int
	NightsBooked     int
	CapturedCents    int64
	PendingCents     int64
	Currency         string
	AverageRating    float64
	ReviewCount      int
}

// Service computes host analytics.
type Service struct {
	properties property.Repository
	bookings   booking.Repository
	payments   payment.Repository
}

// NewService wires the analytics service.
func NewService(properties property.Repository, bookings booking.Repository, payments payment.Repository) *Service {
	return &Service{properties: properties, bookings: bookings, payments: payments}
}

// HostMetrics aggregates figures across all of a host's listings.
func (s *Service) HostMetrics(ctx context.Context, hostID uuid.UUID) (HostMetrics, error) {
	props, err := s.properties.ListByHost(ctx, hostID, reportPage)
	if err != nil {
		return HostMetrics{}, err
	}

	m := HostMetrics{Listings: int(props.Total)}

	var ratingWeighted float64
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var bookingIDs []uuid.UUID

	for _, p := range props.Items {
		if p.Status == property.StatusPublished {
			m.Published++
		}
		ratingWeighted += p.AverageRating * float64(p.ReviewCount)
		m.ReviewCount += p.ReviewCount

		bs, err := s.bookings.ListByProperty(ctx, p.ID, reportPage)
		if err != nil {
			return HostMetrics{}, err
		}
		for _, b := range bs.Items {
			m.Bookings++
			bookingIDs = append(bookingIDs, b.ID)
			switch b.Status {
			case booking.StatusPending:
				m.Pending++
			case booking.StatusConfirmed:
				m.Confirmed++
			case booking.StatusCompleted:
				m.Completed++
			case booking.StatusCancelled:
				m.Cancelled++
			}
			if b.Status == booking.StatusConfirmed || b.Status == booking.StatusCompleted {
				m.NightsBooked += b.Dates.Nights()
			}
			if b.IsActive() && !b.Dates.CheckIn.Before(today) {
				m.UpcomingCheckins++
			}
		}
	}

	if m.ReviewCount > 0 {
		m.AverageRating = ratingWeighted / float64(m.ReviewCount)
	}

	rev, err := s.payments.RevenueForBookings(ctx, bookingIDs)
	if err != nil {
		return HostMetrics{}, err
	}
	m.CapturedCents = rev.CapturedCents
	m.PendingCents = rev.PendingCents
	m.Currency = rev.Currency
	if m.Currency == "" {
		m.Currency = "EUR"
	}

	return m, nil
}
