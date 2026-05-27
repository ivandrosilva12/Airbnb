// Package notificationapp contains notification use cases plus the event
// subscriber that turns domain events into notifications.
package notificationapp

import (
	"context"

	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates notification use cases.
type Service struct {
	repo notification.Repository
}

// NewService wires the notification application service.
func NewService(repo notification.Repository) *Service {
	return &Service{repo: repo}
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
	return s.create(ctx, userID, notification.TypeSavedSearchAlert, title, body, relatedID)
}

// create is the internal helper used by the event subscriber.
func (s *Service) create(ctx context.Context, userID uuid.UUID, t notification.Type, title, body string, relatedID uuid.UUID) error {
	n, err := notification.New(userID, t, title, body, relatedID)
	if err != nil {
		return err
	}
	return s.repo.Create(ctx, n)
}
