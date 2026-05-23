// Package message is the bounded context for host↔guest messaging. A
// Conversation is scoped to a property and the two participants (the property's
// host and an interested guest); Messages belong to a conversation.
package message

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Conversation is the aggregate root for a message thread between a guest and a
// host about a specific property.
type Conversation struct {
	ID            uuid.UUID
	PropertyID    uuid.UUID
	HostID        uuid.UUID
	GuestID       uuid.UUID
	CreatedAt     time.Time
	LastMessageAt time.Time
}

// NewConversation creates a conversation, enforcing that host and guest differ.
func NewConversation(propertyID, hostID, guestID uuid.UUID) (*Conversation, error) {
	if hostID == guestID {
		return nil, shared.NewValidationError("cannot start a conversation with yourself")
	}
	now := time.Now().UTC()
	return &Conversation{
		ID:            uuid.New(),
		PropertyID:    propertyID,
		HostID:        hostID,
		GuestID:       guestID,
		CreatedAt:     now,
		LastMessageAt: now,
	}, nil
}

// HasParticipant reports whether the user takes part in the conversation.
func (c *Conversation) HasParticipant(userID uuid.UUID) bool {
	return c.HostID == userID || c.GuestID == userID
}

// Message is an entity belonging to a Conversation.
type Message struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	SenderID       uuid.UUID
	Body           string
	CreatedAt      time.Time
}

// PostMessage appends a message authored by sender to the conversation,
// enforcing that the sender is a participant and the body is non-empty.
func (c *Conversation) PostMessage(senderID uuid.UUID, body string) (*Message, error) {
	if !c.HasParticipant(senderID) {
		return nil, shared.ErrForbidden
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, shared.NewValidationError("message body is required")
	}
	if len(body) > 4000 {
		return nil, shared.NewValidationError("message is too long")
	}
	now := time.Now().UTC()
	c.LastMessageAt = now
	return &Message{
		ID:             uuid.New(),
		ConversationID: c.ID,
		SenderID:       senderID,
		Body:           body,
		CreatedAt:      now,
	}, nil
}
