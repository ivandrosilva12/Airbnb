// Package messageapp contains host↔guest messaging use cases. It coordinates
// the message and property contexts: a guest starts a thread about a property,
// and the host is derived from that property.
package messageapp

import (
	"context"
	"errors"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates messaging use cases.
type Service struct {
	messages   message.Repository
	properties property.Repository
	events     event.Publisher
}

// NewService wires the messaging application service. publisher may be nil.
func NewService(messages message.Repository, properties property.Repository, publisher event.Publisher) *Service {
	if publisher == nil {
		publisher = event.Nop()
	}
	return &Service{messages: messages, properties: properties, events: publisher}
}

// StartConversation returns the thread between the guest and the property's
// host, creating it on first contact. The host cannot start a thread about
// their own listing (they reply to existing ones).
func (s *Service) StartConversation(ctx context.Context, guestID, propertyID uuid.UUID) (*message.Conversation, error) {
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	if prop.IsOwnedBy(guestID) {
		return nil, shared.NewValidationError("hosts cannot start a conversation on their own property")
	}

	existing, err := s.messages.FindConversationByPropertyAndGuest(ctx, propertyID, guestID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, shared.ErrNotFound) {
		return nil, err
	}

	conv, err := message.NewConversation(propertyID, prop.HostID, guestID)
	if err != nil {
		return nil, err
	}
	if err := s.messages.CreateConversation(ctx, conv); err != nil {
		return nil, err
	}
	return conv, nil
}

// SendMessage posts a message to a conversation; only participants may post.
func (s *Service) SendMessage(ctx context.Context, actorID, conversationID uuid.UUID, body string) (*message.Message, error) {
	conv, err := s.messages.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	msg, err := conv.PostMessage(actorID, body)
	if err != nil {
		return nil, err
	}
	if err := s.messages.AddMessage(ctx, msg); err != nil {
		return nil, err
	}
	if err := s.messages.UpdateConversation(ctx, conv); err != nil {
		return nil, err
	}

	recipient := conv.GuestID
	if actorID == conv.GuestID {
		recipient = conv.HostID
	}
	s.events.Publish(ctx, event.MessageSent{
		ConversationID: conv.ID,
		SenderID:       actorID,
		RecipientID:    recipient,
	})
	return msg, nil
}

// ListConversations returns the conversations the actor participates in.
func (s *Service) ListConversations(ctx context.Context, actorID uuid.UUID, page shared.Page) (shared.PageResult[*message.Conversation], error) {
	return s.messages.ListConversationsForUser(ctx, actorID, page)
}

// ListMessages returns the messages of a conversation the actor participates in.
func (s *Service) ListMessages(ctx context.Context, actorID, conversationID uuid.UUID, page shared.Page) (shared.PageResult[*message.Message], error) {
	conv, err := s.messages.FindConversationByID(ctx, conversationID)
	if err != nil {
		return shared.PageResult[*message.Message]{}, err
	}
	if !conv.HasParticipant(actorID) {
		return shared.PageResult[*message.Message]{}, shared.ErrForbidden
	}
	return s.messages.ListMessages(ctx, conversationID, page)
}
