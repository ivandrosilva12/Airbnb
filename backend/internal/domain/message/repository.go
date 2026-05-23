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

	AddMessage(ctx context.Context, m *Message) error
	ListMessages(ctx context.Context, conversationID uuid.UUID, page shared.Page) (shared.PageResult[*Message], error)
}
