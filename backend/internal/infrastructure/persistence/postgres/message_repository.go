package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MessageRepository is the Postgres implementation of message.Repository.
type MessageRepository struct {
	pool *pgxpool.Pool
}

// NewMessageRepository builds a MessageRepository.
func NewMessageRepository(pool *pgxpool.Pool) *MessageRepository {
	return &MessageRepository{pool: pool}
}

const conversationColumns = `id, property_id, host_id, guest_id, created_at, last_message_at`

func (r *MessageRepository) CreateConversation(ctx context.Context, c *message.Conversation) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO conversations (`+conversationColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		c.ID, c.PropertyID, c.HostID, c.GuestID, c.CreatedAt, c.LastMessageAt,
	)
	return mapError(err)
}

func (r *MessageRepository) UpdateConversation(ctx context.Context, c *message.Conversation) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE conversations SET last_message_at=$2 WHERE id=$1`, c.ID, c.LastMessageAt)
	return mapError(err)
}

func (r *MessageRepository) FindConversationByID(ctx context.Context, id uuid.UUID) (*message.Conversation, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE id=$1`, id)
	return scanConversation(row)
}

func (r *MessageRepository) FindConversationByPropertyAndGuest(ctx context.Context, propertyID, guestID uuid.UUID) (*message.Conversation, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+conversationColumns+` FROM conversations WHERE property_id=$1 AND guest_id=$2`,
		propertyID, guestID)
	return scanConversation(row)
}

func (r *MessageRepository) ListConversationsForUser(ctx context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[*message.Conversation], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM conversations WHERE host_id=$1 OR guest_id=$1`, userID,
	).Scan(&total); err != nil {
		return shared.PageResult[*message.Conversation]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+conversationColumns+` FROM conversations
		WHERE host_id=$1 OR guest_id=$1
		ORDER BY last_message_at DESC LIMIT $2 OFFSET $3`,
		userID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*message.Conversation]{}, mapError(err)
	}
	defer rows.Close()

	var items []*message.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return shared.PageResult[*message.Conversation]{}, err
		}
		items = append(items, c)
	}
	return shared.PageResult[*message.Conversation]{Items: items, Total: total}, mapError(rows.Err())
}

func (r *MessageRepository) AddMessage(ctx context.Context, m *message.Message) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO messages (id, conversation_id, sender_id, body, created_at)
		VALUES ($1,$2,$3,$4,$5)`,
		m.ID, m.ConversationID, m.SenderID, m.Body, m.CreatedAt,
	)
	return mapError(err)
}

func (r *MessageRepository) ListMessages(ctx context.Context, conversationID uuid.UUID, page shared.Page) (shared.PageResult[*message.Message], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE conversation_id=$1`, conversationID,
	).Scan(&total); err != nil {
		return shared.PageResult[*message.Message]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, conversation_id, sender_id, body, created_at
		FROM messages WHERE conversation_id=$1
		ORDER BY created_at ASC LIMIT $2 OFFSET $3`,
		conversationID, page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*message.Message]{}, mapError(err)
	}
	defer rows.Close()

	var items []*message.Message
	for rows.Next() {
		var m message.Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.CreatedAt); err != nil {
			return shared.PageResult[*message.Message]{}, mapError(err)
		}
		items = append(items, &m)
	}
	return shared.PageResult[*message.Message]{Items: items, Total: total}, mapError(rows.Err())
}

func scanConversation(row rowScanner) (*message.Conversation, error) {
	var c message.Conversation
	err := row.Scan(&c.ID, &c.PropertyID, &c.HostID, &c.GuestID, &c.CreatedAt, &c.LastMessageAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &c, nil
}
