// Package arrivalapp drives the "your check-in details are now available"
// notification (S102 — WF-GAP-007). The scheduler ticks this on a short
// cadence; each tick scans confirmed bookings whose check-in falls inside
// the next 48 hours and creates a notification per guest the first time
// the booking crosses the reveal window.
//
// Dedup happens via the notification repository: the same (guest, type,
// related-booking) tuple is never inserted twice, so retries from a slow
// previous tick don't cause double-pings.
package arrivalapp

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/google/uuid"
)

// Notifier is the subset of notificationapp.Service the scheduler needs.
type Notifier interface {
	ExistsForUser(ctx context.Context, userID uuid.UUID, t notification.Type, relatedID uuid.UUID) (bool, error)
	NotifyArrivalAvailable(ctx context.Context, guestID, bookingID uuid.UUID, propertyTitle string) error
}

// Service runs the periodic sweep.
type Service struct {
	bookings   booking.Repository
	properties property.Repository
	notif      Notifier
	// clock returns "now" — replaceable so unit tests pin a specific
	// instant inside the 48h reveal window.
	clock func() time.Time
}

// NewService wires the arrival-info scheduler service.
func NewService(bookings booking.Repository, properties property.Repository, notif Notifier) *Service {
	return &Service{bookings: bookings, properties: properties, notif: notif, clock: time.Now}
}

// WithClock injects a fake clock for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.clock = now
	return s
}

// Run sweeps confirmed bookings whose check-in falls in [now, now+48h] and
// creates an arrival-info notification for each guest that hasn't been
// notified yet. Returns the count of notifications created.
//
// The window is the full 48h reveal slot rather than a narrow "just-crossed"
// strip because the scheduler may have missed a tick (long startup, paused
// container, etc) and the dedupe key — (guest, type=arrival_info_available,
// related=bookingID) — guarantees only-once delivery anyway.
func (s *Service) Run(ctx context.Context) (int, error) {
	now := s.clock().UTC()
	end := now.Add(property.ArrivalRevealWindow) // 48h
	bookings, err := s.bookings.ListConfirmedStartingBetween(ctx, now, end)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, b := range bookings {
		exists, err := s.notif.ExistsForUser(ctx, b.GuestID, notification.TypeArrivalInfoAvailable, b.ID)
		if err != nil {
			slog.Warn("arrival: dedupe lookup failed; skipping", "booking", b.ID, "error", err)
			continue
		}
		if exists {
			continue
		}
		title := s.lookupTitle(ctx, b.PropertyID)
		if err := s.notif.NotifyArrivalAvailable(ctx, b.GuestID, b.ID, title); err != nil {
			slog.Warn("arrival: notify failed", "booking", b.ID, "guest", b.GuestID, "error", err)
			continue
		}
		sent++
	}
	return sent, nil
}

// lookupTitle resolves the listing's title for the notification body.
// Best-effort: a deleted/unreadable property yields "" and the notifier
// falls back to a generic phrase.
func (s *Service) lookupTitle(ctx context.Context, propertyID uuid.UUID) string {
	if s.properties == nil {
		return ""
	}
	p, err := s.properties.FindByID(ctx, propertyID)
	if err != nil || p == nil {
		// Don't log a not-found — it's noise during normal soft-deletes.
		if err != nil && !errors.Is(err, errNotFoundSentinel) {
			// Reserved branch — kept compiled but no log to keep arrival
			// scheduler quiet under churn.
			_ = err
		}
		return ""
	}
	return p.Title
}

// errNotFoundSentinel exists so the lookupTitle branch above has a typed
// reference; the property repo's ErrNotFound lives in domain/shared, and
// importing shared here just to silence one log path felt like overreach.
var errNotFoundSentinel = errors.New("not found")
