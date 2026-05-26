// Package notification is the bounded context for in-app notifications. A
// Notification is delivered to a single recipient and references the resource
// that triggered it.
package notification

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Type classifies a notification so clients can route or icon it.
type Type string

const (
	TypeBookingRequested Type = "booking_requested"
	TypeBookingConfirmed Type = "booking_confirmed"
	TypeBookingCancelled Type = "booking_cancelled"
	TypeBookingModified  Type = "booking_modified"
	TypeMessageReceived  Type = "message_received"
	TypeIdentityVerified Type = "identity_verified"
	TypeReviewRequested  Type = "review_requested"
)

// Notification is the aggregate root for a single in-app notification.
type Notification struct {
	ID        uuid.UUID
	UserID    uuid.UUID // recipient
	Type      Type
	Title     string
	Body      string
	RelatedID uuid.UUID // related resource (booking, conversation, …); may be Nil
	ReadAt    *time.Time
	CreatedAt time.Time
}

// New builds a Notification for a recipient, enforcing basic invariants.
func New(userID uuid.UUID, t Type, title, body string, relatedID uuid.UUID) (*Notification, error) {
	title = strings.TrimSpace(title)
	if userID == uuid.Nil {
		return nil, shared.NewValidationError("notification requires a recipient")
	}
	if title == "" {
		return nil, shared.NewValidationError("notification requires a title")
	}
	return &Notification{
		ID:        uuid.New(),
		UserID:    userID,
		Type:      t,
		Title:     title,
		Body:      strings.TrimSpace(body),
		RelatedID: relatedID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// MarkRead stamps the notification as read (idempotent).
func (n *Notification) MarkRead() {
	if n.ReadAt == nil {
		now := time.Now().UTC()
		n.ReadAt = &now
	}
}

// MarkUnread clears the read stamp so the notification resurfaces as unread
// (idempotent).
func (n *Notification) MarkUnread() {
	n.ReadAt = nil
}

// IsRead reports whether the notification has been read.
func (n *Notification) IsRead() bool { return n.ReadAt != nil }
