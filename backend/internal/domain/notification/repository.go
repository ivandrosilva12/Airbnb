package notification

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the notification context.
type Repository interface {
	Create(ctx context.Context, n *Notification) error
	ListByUser(ctx context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[*Notification], error)
	UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error)
	// MarkRead marks one notification read; it must belong to the user. Returns
	// ErrNotFound if no matching notification exists.
	MarkRead(ctx context.Context, id, userID uuid.UUID) error
	// MarkUnread clears the read stamp on one notification; it must belong to the
	// user. Returns ErrNotFound if no matching notification exists.
	MarkUnread(ctx context.Context, id, userID uuid.UUID) error
	MarkAllRead(ctx context.Context, userID uuid.UUID) error
	// DeleteByUser removes all of a user's notifications (GDPR erasure).
	DeleteByUser(ctx context.Context, userID uuid.UUID) error
	// ExistsByUserTypeAndRelated reports whether a notification of the
	// given (user, type, related) tuple already exists. Used as a dedupe
	// key by the arrival-info scheduler (S102 — WF-GAP-007) so a guest
	// gets at most one "arrival info is now available" notice per
	// booking, even if the scheduler ticks multiple times inside the
	// 48-hour reveal window.
	ExistsByUserTypeAndRelated(ctx context.Context, userID uuid.UUID, t Type, relatedID uuid.UUID) (bool, error)
}
