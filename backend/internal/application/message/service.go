// Package messageapp contains host↔guest messaging use cases. It coordinates
// the message and property contexts: a guest starts a thread about a property,
// and the host is derived from that property.
package messageapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/userblock"
	"github.com/google/uuid"
)

// Service orchestrates messaging use cases.
type Service struct {
	messages   message.Repository
	properties property.Repository
	blocks     userblock.Repository
	storage    port.Storage
	uow        port.UnitOfWork
}

// NewService wires the messaging application service. The UnitOfWork makes the
// message write and its MessageSent event commit atomically; storage backs
// message attachments; blocks gates contact between users who blocked each other.
func NewService(messages message.Repository, properties property.Repository, blocks userblock.Repository, storage port.Storage, uow port.UnitOfWork) *Service {
	return &Service{messages: messages, properties: properties, blocks: blocks, storage: storage, uow: uow}
}

// ensureNotBlocked refuses contact when either party has blocked the other.
func (s *Service) ensureNotBlocked(ctx context.Context, a, b uuid.UUID) error {
	blocked, err := s.blocks.IsBlocked(ctx, a, b)
	if err != nil {
		return err
	}
	if blocked {
		return shared.NewValidationError("messaging is unavailable between you and this user")
	}
	return nil
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
	if err := s.ensureNotBlocked(ctx, guestID, prop.HostID); err != nil {
		return nil, err
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

// SendMessage posts a text message to a conversation; only participants may post.
func (s *Service) SendMessage(ctx context.Context, actorID, conversationID uuid.UUID, body string) (*message.Message, error) {
	conv, err := s.messages.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureNotBlocked(ctx, conv.HostID, conv.GuestID); err != nil {
		return nil, err
	}
	msg, err := conv.PostMessage(actorID, body)
	if err != nil {
		return nil, err
	}
	if err := s.persist(ctx, conv, msg, actorID); err != nil {
		return nil, err
	}
	return msg, nil
}

// AttachmentInput carries an uploaded file (and optional caption) to attach to a
// conversation.
type AttachmentInput struct {
	Body        string
	Reader      io.Reader
	Size        int64
	ContentType string
	Filename    string
}

// SendAttachment uploads a file to object storage and posts it as a message to
// the conversation; only participants may post. The participant check runs
// before the upload so a forbidden actor never leaves an orphaned object.
func (s *Service) SendAttachment(ctx context.Context, actorID, conversationID uuid.UUID, in AttachmentInput) (*message.Message, error) {
	conv, err := s.messages.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if !conv.HasParticipant(actorID) {
		return nil, shared.ErrForbidden
	}
	if err := s.ensureNotBlocked(ctx, conv.HostID, conv.GuestID); err != nil {
		return nil, err
	}
	objectKey := fmt.Sprintf("attachments/%s/%s%s", conversationID, uuid.NewString(), path.Ext(in.Filename))
	url, err := s.storage.Upload(ctx, objectKey, in.Reader, in.Size, in.ContentType)
	if err != nil {
		return nil, err
	}
	msg, err := conv.PostAttachment(actorID, in.Body, message.Attachment{
		URL:         url,
		ContentType: in.ContentType,
		Filename:    in.Filename,
		Size:        in.Size,
	})
	if err != nil {
		return nil, err
	}
	if err := s.persist(ctx, conv, msg, actorID); err != nil {
		return nil, err
	}
	return msg, nil
}

// persist atomically writes a new message, bumps the conversation, and enqueues
// the MessageSent event so live updates and notifications fan out.
func (s *Service) persist(ctx context.Context, conv *message.Conversation, msg *message.Message, actorID uuid.UUID) error {
	recipient := conv.GuestID
	if actorID == conv.GuestID {
		recipient = conv.HostID
	}
	return s.uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.Messages.AddMessage(ctx, msg); err != nil {
			return err
		}
		if err := tx.Messages.UpdateConversation(ctx, conv); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.MessageSent{
			ConversationID: conv.ID,
			SenderID:       actorID,
			RecipientID:    recipient,
		})
		if err != nil {
			return err
		}
		return tx.Outbox.Append(ctx, rec)
	})
}

// ListConversations returns the conversations the actor participates in.
func (s *Service) ListConversations(ctx context.Context, actorID uuid.UUID, page shared.Page) (shared.PageResult[*message.Conversation], error) {
	return s.messages.ListConversationsForUser(ctx, actorID, page)
}

// UnreadByConversation returns the actor's unread count keyed by conversation.
func (s *Service) UnreadByConversation(ctx context.Context, actorID uuid.UUID) (map[uuid.UUID]int64, error) {
	return s.messages.ConversationUnreadCounts(ctx, actorID)
}

// TotalUnread returns the actor's total unread message count.
func (s *Service) TotalUnread(ctx context.Context, actorID uuid.UUID) (int64, error) {
	return s.messages.TotalUnread(ctx, actorID)
}

// MarkRead marks a conversation read up to now for the actor (a participant).
func (s *Service) MarkRead(ctx context.Context, actorID, conversationID uuid.UUID) error {
	conv, err := s.messages.FindConversationByID(ctx, conversationID)
	if err != nil {
		return err
	}
	if err := conv.MarkReadBy(actorID); err != nil {
		return err
	}
	return s.messages.UpdateConversation(ctx, conv)
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
