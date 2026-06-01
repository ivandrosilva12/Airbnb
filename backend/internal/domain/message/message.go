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

// MarkReadByHost advances the host's read marker without requiring the caller
// to *be* the host. Used by the application layer when a co-host with the
// reply_messages permission reads or posts on the host's behalf — the host's
// unread count drops because their team has handled the thread.
func (c *Conversation) MarkReadByHost() {
	now := time.Now().UTC()
	c.HostLastReadAt = &now
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
	return c.buildTextMessage(senderID, body)
}

// PostMessageOnBehalfOfHost is the variant for an externally-authorised
// sender (a co-host with the reply_messages permission). The senderID is
// recorded so the message history reflects who actually replied, but the
// host's read marker is bumped to now — from the host's mailbox view their
// team handled the thread, so it doesn't show as unread to them. Callers
// must have independently verified that senderID may post on this listing;
// the aggregate does not re-check that.
func (c *Conversation) PostMessageOnBehalfOfHost(senderID uuid.UUID, body string) (*Message, error) {
	msg, err := c.buildTextMessage(senderID, body)
	if err != nil {
		return nil, err
	}
	c.HostLastReadAt = &msg.CreatedAt
	return msg, nil
}

// PostAttachment appends a message carrying an attachment (with an optional
// caption) authored by sender. The body may be empty since the attachment is
// the content, but the attachment itself must be present.
func (c *Conversation) PostAttachment(senderID uuid.UUID, body string, att Attachment) (*Message, error) {
	if !c.HasParticipant(senderID) {
		return nil, shared.ErrForbidden
	}
	return c.buildAttachmentMessage(senderID, body, att)
}

// PostAttachmentOnBehalfOfHost is the co-host equivalent of PostAttachment.
// See PostMessageOnBehalfOfHost for the authorisation contract and read-marker
// behaviour.
func (c *Conversation) PostAttachmentOnBehalfOfHost(senderID uuid.UUID, body string, att Attachment) (*Message, error) {
	msg, err := c.buildAttachmentMessage(senderID, body, att)
	if err != nil {
		return nil, err
	}
	c.HostLastReadAt = &msg.CreatedAt
	return msg, nil
}

// buildTextMessage enforces body invariants and stamps a new Message. Callers
// guarantee senderID is authorised — used by both the strict and on-behalf
// paths above.
func (c *Conversation) buildTextMessage(senderID uuid.UUID, body string) (*Message, error) {
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

// buildAttachmentMessage enforces attachment invariants and stamps a new
// Message. See buildTextMessage for the authorisation contract.
func (c *Conversation) buildAttachmentMessage(senderID uuid.UUID, body string, att Attachment) (*Message, error) {
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
