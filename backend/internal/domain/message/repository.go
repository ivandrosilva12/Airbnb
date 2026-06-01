package message

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the messaging context.
type Repository interface {
	CreateConversation(ctx context.Context, c *Conversation) error
	UpdateConversation(ctx context.Context, c *Conversation) error
	FindConversationByID(ctx context.Context, id uuid.UUID) (*Conversation, error)
	// FindConversationByPropertyAndGuest returns the existing thread for a
	// (property, guest) pair, or ErrNotFound. Used to avoid duplicate threads.
	FindConversationByPropertyAndGuest(ctx context.Context, propertyID, guestID uuid.UUID) (*Conversation, error)
	ListConversationsForUser(ctx context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[*Conversation], error)
	// ListConversationsByProperties returns every conversation attached to
	// one of the given property ids, ordered newest-activity-first. Used by
	// the co-host messaging surface to surface the team mailbox; an empty
	// id slice returns an empty page without hitting the store.
	ListConversationsByProperties(ctx context.Context, propertyIDs []uuid.UUID, page shared.Page) (shared.PageResult[*Conversation], error)

	AddMessage(ctx context.Context, m *Message) error
	ListMessages(ctx context.Context, conversationID uuid.UUID, page shared.Page) (shared.PageResult[*Message], error)

	// ConversationUnreadCounts returns, per conversation the user participates in,
	// the number of messages from the other party newer than the user's read
	// marker. Conversations with zero unread may be omitted.
	ConversationUnreadCounts(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int64, error)
	// TotalUnread is the user's total unread message count across conversations.
	TotalUnread(ctx context.Context, userID uuid.UUID) (int64, error)
}
