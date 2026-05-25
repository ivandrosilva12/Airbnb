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
// host about a specific property. Each participant has a "last read" timestamp
// used to derive unread counts.
type Conversation struct {
	ID              uuid.UUID
	PropertyID      uuid.UUID
	HostID          uuid.UUID
	GuestID         uuid.UUID
	CreatedAt       time.Time
	LastMessageAt   time.Time
	HostLastReadAt  *time.Time
	GuestLastReadAt *time.Time
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

// MarkReadBy advances the given participant's read marker to now, so messages
// up to this point are no longer counted as unread for them.
func (c *Conversation) MarkReadBy(userID uuid.UUID) error {
	now := time.Now().UTC()
	switch userID {
	case c.HostID:
		c.HostLastReadAt = &now
	case c.GuestID:
		c.GuestLastReadAt = &now
	default:
		return shared.ErrForbidden
	}
	return nil
}

// LastReadFor returns the participant's read marker (nil if never read).
func (c *Conversation) LastReadFor(userID uuid.UUID) *time.Time {
	if userID == c.HostID {
		return c.HostLastReadAt
	}
	if userID == c.GuestID {
		return c.GuestLastReadAt
	}
	return nil
}

// Attachment is an optional file (image or document) carried by a message. The
// bytes live in object storage; the message only holds its metadata and URL.
type Attachment struct {
	URL         string
	ContentType string
	Filename    string
	Size        int64
}

// Message is an entity belonging to a Conversation. It carries text, an
// attachment, or both — at least one is always present.
type Message struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	SenderID       uuid.UUID
	Body           string
	Attachment     *Attachment
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

// PostAttachment appends a message carrying an attachment (with an optional
// caption) authored by sender. The body may be empty since the attachment is
// the content, but the attachment itself must be present.
func (c *Conversation) PostAttachment(senderID uuid.UUID, body string, att Attachment) (*Message, error) {
	if !c.HasParticipant(senderID) {
		return nil, shared.ErrForbidden
	}
	if att.URL == "" {
		return nil, shared.NewValidationError("attachment is required")
	}
	body = strings.TrimSpace(body)
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
		Attachment:     &att,
		CreatedAt:      now,
	}, nil
}
