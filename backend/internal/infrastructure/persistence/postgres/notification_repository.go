package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NotificationRepository is the Postgres implementation of notification.Repository.
type NotificationRepository struct {
	pool *pgxpool.Pool
}

// NewNotificationRepository builds a NotificationRepository.
func NewNotificationRepository(pool *pgxpool.Pool) *NotificationRepository {
	return &NotificationRepository{pool: pool}
}

func (r *NotificationRepository) Create(ctx context.Context, n *notification.Notification) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO notifications (id, user_id, type, title, body, related_id, read_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		n.ID, n.UserID, string(n.Type), n.Title, n.Body, nilUUID(n.RelatedID), n.ReadAt, n.CreatedAt,
	)
	return mapError(err)
}

func (r *NotificationRepository) ListByUser(ctx context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[*notification.Notification], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE user_id=$1`, userID,
	).Scan(&total); err != nil {
		return shared.PageResult[*notification.Notification]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, type, title, body, related_id, read_at, created_at
		FROM notifications WHERE user_id=$1 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		userID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*notification.Notification]{}, mapError(err)
	}
	defer rows.Close()

	var items []*notification.Notification
	for rows.Next() {
		var (
			n         notification.Notification
			typ       string
			relatedID *uuid.UUID
		)
		if err := rows.Scan(&n.ID, &n.UserID, &typ, &n.Title, &n.Body, &relatedID, &n.ReadAt, &n.CreatedAt); err != nil {
			return shared.PageResult[*notification.Notification]{}, mapError(err)
		}
		n.Type = notification.Type(typ)
		if relatedID != nil {
			n.RelatedID = *relatedID
		}
		items = append(items, &n)
	}
	return shared.PageResult[*notification.Notification]{Items: items, Total: total}, mapError(rows.Err())
}

func (r *NotificationRepository) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE user_id=$1 AND read_at IS NULL`, userID,
	).Scan(&count)
	return count, mapError(err)
}

func (r *NotificationRepository) MarkRead(ctx context.Context, id, userID uuid.UUID) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE notifications SET read_at=now() WHERE id=$1 AND user_id=$2 AND read_at IS NULL`,
		id, userID,
	)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		// Either it doesn't exist / isn't theirs, or it was already read. Treat a
		// missing row as not-found; an already-read row is a harmless no-op.
		var exists bool
		if e := r.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM notifications WHERE id=$1 AND user_id=$2)`, id, userID,
		).Scan(&exists); e != nil {
			return mapError(e)
		}
		if !exists {
			return shared.ErrNotFound
		}
	}
	return nil
}

func (r *NotificationRepository) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notifications SET read_at=now() WHERE user_id=$1 AND read_at IS NULL`, userID)
	return mapError(err)
}

func (r *NotificationRepository) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM notifications WHERE user_id=$1`, userID)
	return mapError(err)
}

// nilUUID converts the zero UUID to nil so it stores as SQL NULL.
func nilUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
