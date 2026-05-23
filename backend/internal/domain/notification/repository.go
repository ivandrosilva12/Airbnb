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
	MarkAllRead(ctx context.Context, userID uuid.UUID) error
}
