package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// MessageRepository is the Postgres implementation of message.Repository. It
// runs against a querier (the pool, or a transaction inside a UnitOfWork).
type MessageRepository struct {
	pool querier
}

// NewMessageRepository builds a MessageRepository.
func NewMessageRepository(db querier) *MessageRepository {
	return &MessageRepository{pool: db}
}

const conversationColumns = `id, property_id, host_id, guest_id, created_at, last_message_at,
	host_last_read_at, guest_last_read_at`

func (r *MessageRepository) CreateConversation(ctx context.Context, c *message.Conversation) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO conversations (`+conversationColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		c.ID, c.PropertyID, c.HostID, c.GuestID, c.CreatedAt, c.LastMessageAt,
		c.HostLastReadAt, c.GuestLastReadAt,
	)
	return mapError(err)
}

func (r *MessageRepository) UpdateConversation(ctx context.Context, c *message.Conversation) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE conversations SET last_message_at=$2, host_last_read_at=$3, guest_last_read_at=$4 WHERE id=$1`,
		c.ID, c.LastMessageAt, c.HostLastReadAt, c.GuestLastReadAt)
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

func (r *MessageRepository) ListConversationsByProperties(ctx context.Context, propertyIDs []uuid.UUID, page shared.Page) (shared.PageResult[*message.Conversation], error) {
	if len(propertyIDs) == 0 {
		return shared.PageResult[*message.Conversation]{}, nil
	}
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM conversations WHERE property_id = ANY($1)`, propertyIDs,
	).Scan(&total); err != nil {
		return shared.PageResult[*message.Conversation]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+conversationColumns+` FROM conversations
		WHERE property_id = ANY($1)
		ORDER BY last_message_at DESC LIMIT $2 OFFSET $3`,
		propertyIDs, page.Limit, page.Offset,
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
	var url, ctype, name *string
	var size *int64
	if m.Attachment != nil {
		url = &m.Attachment.URL
		ctype = &m.Attachment.ContentType
		name = &m.Attachment.Filename
		size = &m.Attachment.Size
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO messages (id, conversation_id, sender_id, body, created_at, attachment_url, attachment_type, attachment_name, attachment_size)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		m.ID, m.ConversationID, m.SenderID, m.Body, m.CreatedAt, url, ctype, name, size,
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
		SELECT id, conversation_id, sender_id, body, created_at, attachment_url, attachment_type, attachment_name, attachment_size
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
		m, err := scanMessage(rows)
		if err != nil {
			return shared.PageResult[*message.Message]{}, mapError(err)
		}
		items = append(items, m)
	}
	return shared.PageResult[*message.Message]{Items: items, Total: total}, mapError(rows.Err())
}

func scanMessage(row rowScanner) (*message.Message, error) {
	var (
		m                message.Message
		url, ctype, name *string
		size             *int64
	)
	if err := row.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.CreatedAt, &url, &ctype, &name, &size); err != nil {
		return nil, err
	}
	if url != nil {
		m.Attachment = &message.Attachment{URL: *url}
		if ctype != nil {
			m.Attachment.ContentType = *ctype
		}
		if name != nil {
			m.Attachment.Filename = *name
		}
		if size != nil {
			m.Attachment.Size = *size
		}
	}
	return &m, nil
}

func scanConversation(row rowScanner) (*message.Conversation, error) {
	var c message.Conversation
	err := row.Scan(&c.ID, &c.PropertyID, &c.HostID, &c.GuestID, &c.CreatedAt, &c.LastMessageAt,
		&c.HostLastReadAt, &c.GuestLastReadAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &c, nil
}

// unreadFilter is the WHERE clause counting messages newer than the user's
// per-conversation read marker, sent by the other party.
const unreadFilter = `
	JOIN conversations c ON c.id = m.conversation_id
	WHERE (c.host_id = $1 OR c.guest_id = $1)
	  AND m.sender_id <> $1
	  AND m.created_at > COALESCE(
	        CASE WHEN c.host_id = $1 THEN c.host_last_read_at ELSE c.guest_last_read_at END,
	        '-infinity'::timestamptz)`

func (r *MessageRepository) ConversationUnreadCounts(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int64, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT m.conversation_id, count(*) FROM messages m`+unreadFilter+` GROUP BY m.conversation_id`,
		userID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]int64)
	for rows.Next() {
		var (
			id  uuid.UUID
			cnt int64
		)
		if err := rows.Scan(&id, &cnt); err != nil {
			return nil, mapError(err)
		}
		out[id] = cnt
	}
	return out, mapError(rows.Err())
}

func (r *MessageRepository) TotalUnread(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages m`+unreadFilter, userID).Scan(&count)
	return count, mapError(err)
}
