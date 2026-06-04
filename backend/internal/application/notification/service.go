// Package notificationapp contains notification use cases plus the event
// subscriber that turns domain events into notifications.
package notificationapp

import (
	"context"
	"github.com/airhost/backend/internal/observability/logctx"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/google/uuid"
)

// PushCategory buckets a push so per-user opt-outs can be applied. The values
// must match pushtokenapp.Category but are duplicated here so this package does
// not depend on the push application package (avoiding a cycle when push wires
// notifications back through the user repo).
type PushCategory string

const (
	PushCatBookings PushCategory = "bookings"
	PushCatMessages PushCategory = "messages"
	PushCatAccount  PushCategory = "account"
)

// PushNotifier is the outbound port the notification subscriber uses to fan a
// just-created notification out to the recipient's mobile/web devices. The
// implementation (pushtokenapp.Service) looks up the user's tokens and routes
// per platform; nil disables push delivery (used in tests or when the slice is
// not wired).
type PushNotifier interface {
	Push(ctx context.Context, userID uuid.UUID, cat string, title, body string, data map[string]string) error
}

// Service orchestrates notification use cases.
type Service struct {
	repo     notification.Repository
	push     PushNotifier            // optional
	bookings booking.Repository      // optional — only used by the split-payment subscriber
	splits   splitpayment.Repository // optional — only used by the split-payment subscriber
}

// NewService wires the notification application service.
func NewService(repo notification.Repository) *Service {
	return &Service{repo: repo}
}

// WithPush attaches a PushNotifier so newly-created notifications are also
// mirrored as native pushes. Returns the same service for chained wiring.
func (s *Service) WithPush(p PushNotifier) *Service {
	s.push = p
	return s
}

// WithBookings attaches a booking repository so the event subscriber can look
// up booking-context fields (organizer / GuestID) on events that carry only
// the booking id. Optional — handlers that need it short-circuit when nil.
func (s *Service) WithBookings(r booking.Repository) *Service {
	s.bookings = r
	return s
}

// WithSplitPayments attaches a split-payment repository so the event
// subscriber can fan out SplitPaymentCompleted notifications to every payer.
// Optional — the handler short-circuits when nil so existing call sites keep
// working without re-wiring.
func (s *Service) WithSplitPayments(r splitpayment.Repository) *Service {
	s.splits = r
	return s
}

// List returns a recipient's notifications.
func (s *Service) List(ctx context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[*notification.Notification], error) {
	return s.repo.ListByUser(ctx, userID, page)
}

// UnreadCount returns the number of unread notifications for a recipient.
func (s *Service) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.repo.UnreadCount(ctx, userID)
}

// MarkRead marks a single notification read.
func (s *Service) MarkRead(ctx context.Context, userID, id uuid.UUID) error {
	return s.repo.MarkRead(ctx, id, userID)
}

// MarkUnread marks a single notification unread again.
func (s *Service) MarkUnread(ctx context.Context, userID, id uuid.UUID) error {
	return s.repo.MarkUnread(ctx, id, userID)
}

// MarkAllRead marks every notification for the user read.
func (s *Service) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	return s.repo.MarkAllRead(ctx, userID)
}

// Notify delivers a saved-search alert notification to a user. It is the public
// entry used by the saved-search alert job (other notifications are produced by
// the event subscriber).
func (s *Service) Notify(ctx context.Context, userID uuid.UUID, title, body string, relatedID uuid.UUID) error {
	return s.create(ctx, userID, notification.TypeSavedSearchAlert, title, body, relatedID, PushCatAccount)
}

// create is the internal helper used by the event subscriber. cat picks the
// push opt-out bucket so a user who muted bookings still receives messages and
// vice-versa.
func (s *Service) create(ctx context.Context, userID uuid.UUID, t notification.Type, title, body string, relatedID uuid.UUID, cat PushCategory) error {
	n, err := notification.New(userID, t, title, body, relatedID)
	if err != nil {
		return err
	}
	if err := s.repo.Create(ctx, n); err != nil {
		return err
	}
	if s.push != nil {
		data := map[string]string{
			"notificationId": n.ID.String(),
			"type":           string(t),
		}
		if relatedID != uuid.Nil {
			data["relatedId"] = relatedID.String()
		}
		if err := s.push.Push(ctx, userID, string(cat), title, body, data); err != nil {
			logctx.LoggerFrom(ctx).Warn("notification: push delivery failed", "user", userID, "type", t, "error", err)
		}
	}
	return nil
}
