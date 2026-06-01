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

// CohostAuthorizer admits a co-host with the requested permission on a
// listing they don't own. The messaging service consults it when the caller
// is neither the conversation's guest nor host — admitting a co-host with
// the reply_messages permission to act on the host's behalf.
type CohostAuthorizer interface {
	Authorize(ctx context.Context, actorID, propertyID uuid.UUID, required property.CohostPermission) (*property.Property, error)
	ListPropertyIDsWithPermission(ctx context.Context, userID uuid.UUID, required property.CohostPermission) ([]uuid.UUID, error)
}

// Service orchestrates messaging use cases.
type Service struct {
	messages   message.Repository
	properties property.Repository
	blocks     userblock.Repository
	storage    port.Storage
	uow        port.UnitOfWork
	cohosts    CohostAuthorizer
}

// NewService wires the messaging application service. The UnitOfWork makes the
// message write and its MessageSent event commit atomically; storage backs
// message attachments; blocks gates contact between users who blocked each other.
func NewService(messages message.Repository, properties property.Repository, blocks userblock.Repository, storage port.Storage, uow port.UnitOfWork) *Service {
	return &Service{messages: messages, properties: properties, blocks: blocks, storage: storage, uow: uow}
}

// WithCohosts plugs in the relaxed gate so a co-host with reply_messages can
// read and reply to threads on listings they help manage. Returns the
// receiver for chaining.
func (s *Service) WithCohosts(c CohostAuthorizer) *Service {
	s.cohosts = c
	return s
}

// authorizeAction is the conversation-level gate: it admits a literal
// participant (host/guest) directly, otherwise asks the cohost authorizer
// whether actorID has the required permission on the conversation's
// property. Returns (asCohost, error): asCohost=true means the actor is a
// co-host acting on the host's behalf, so write paths route through the
// on-behalf methods that keep the host's read marker advanced.
func (s *Service) authorizeAction(ctx context.Context, conv *message.Conversation, actorID uuid.UUID, required property.CohostPermission) (asCohost bool, err error) {
	if conv.HasParticipant(actorID) {
		return false, nil
	}
	if s.cohosts == nil {
		return false, shared.ErrForbidden
	}
	if _, err := s.cohosts.Authorize(ctx, actorID, conv.PropertyID, required); err != nil {
		return false, err
	}
	return true, nil
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

// SendMessage posts a text message to a conversation. The conversation's
// guest and host may post directly; a co-host with the reply_messages
// permission on the listing may post on the host's behalf.
func (s *Service) SendMessage(ctx context.Context, actorID, conversationID uuid.UUID, body string) (*message.Message, error) {
	conv, err := s.messages.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	asCohost, err := s.authorizeAction(ctx, conv, actorID, property.PermReplyMessages)
	if err != nil {
		return nil, err
	}
	if err := s.ensureNotBlocked(ctx, conv.HostID, conv.GuestID); err != nil {
		return nil, err
	}
	var msg *message.Message
	if asCohost {
		msg, err = conv.PostMessageOnBehalfOfHost(actorID, body)
	} else {
		msg, err = conv.PostMessage(actorID, body)
	}
	if err != nil {
		return nil, err
	}
	if err := s.persist(ctx, conv, msg, actorID, asCohost); err != nil {
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
// the conversation. Same auth rules as SendMessage. The authorisation check
// runs before the upload so a forbidden actor never leaves an orphaned object.
func (s *Service) SendAttachment(ctx context.Context, actorID, conversationID uuid.UUID, in AttachmentInput) (*message.Message, error) {
	conv, err := s.messages.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	asCohost, err := s.authorizeAction(ctx, conv, actorID, property.PermReplyMessages)
	if err != nil {
		return nil, err
	}
	if err := s.ensureNotBlocked(ctx, conv.HostID, conv.GuestID); err != nil {
		return nil, err
	}
	objectKey := fmt.Sprintf("attachments/%s/%s%s", conversationID, uuid.NewString(), path.Ext(in.Filename))
	url, err := s.storage.Upload(ctx, objectKey, in.Reader, in.Size, in.ContentType)
	if err != nil {
		return nil, err
	}
	att := message.Attachment{
		URL:         url,
		ContentType: in.ContentType,
		Filename:    in.Filename,
		Size:        in.Size,
	}
	var msg *message.Message
	if asCohost {
		msg, err = conv.PostAttachmentOnBehalfOfHost(actorID, in.Body, att)
	} else {
		msg, err = conv.PostAttachment(actorID, in.Body, att)
	}
	if err != nil {
		return nil, err
	}
	if err := s.persist(ctx, conv, msg, actorID, asCohost); err != nil {
		return nil, err
	}
	return msg, nil
}

// persist atomically writes a new message, bumps the conversation, and enqueues
// the MessageSent event so live updates and notifications fan out. When the
// actor is a co-host (asCohost=true), the recipient is always the guest — the
// notification follows the host side of the thread regardless of which member
// of the host's team replied.
func (s *Service) persist(ctx context.Context, conv *message.Conversation, msg *message.Message, actorID uuid.UUID, asCohost bool) error {
	recipient := conv.GuestID
	if !asCohost && actorID == conv.GuestID {
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

// MarkRead marks a conversation read up to now. A literal participant
// advances their own marker; a co-host with reply_messages advances the
// host's marker (the team has handled the thread).
func (s *Service) MarkRead(ctx context.Context, actorID, conversationID uuid.UUID) error {
	conv, err := s.messages.FindConversationByID(ctx, conversationID)
	if err != nil {
		return err
	}
	asCohost, err := s.authorizeAction(ctx, conv, actorID, property.PermReplyMessages)
	if err != nil {
		return err
	}
	if asCohost {
		conv.MarkReadByHost()
	} else if err := conv.MarkReadBy(actorID); err != nil {
		return err
	}
	return s.messages.UpdateConversation(ctx, conv)
}

// ListMessages returns the messages of a conversation the actor may read —
// a literal participant or a co-host with reply_messages on the listing.
func (s *Service) ListMessages(ctx context.Context, actorID, conversationID uuid.UUID, page shared.Page) (shared.PageResult[*message.Message], error) {
	conv, err := s.messages.FindConversationByID(ctx, conversationID)
	if err != nil {
		return shared.PageResult[*message.Message]{}, err
	}
	if _, err := s.authorizeAction(ctx, conv, actorID, property.PermReplyMessages); err != nil {
		return shared.PageResult[*message.Message]{}, err
	}
	return s.messages.ListMessages(ctx, conversationID, page)
}

// ListCohostConversations returns the threads on listings where the caller
// is a co-host with reply_messages — the "team mailbox" view. Conversations
// where the caller is a literal participant are NOT included (they appear
// in ListConversations instead) so the surfaces stay distinct.
func (s *Service) ListCohostConversations(ctx context.Context, actorID uuid.UUID, page shared.Page) (shared.PageResult[*message.Conversation], error) {
	if s.cohosts == nil {
		return shared.PageResult[*message.Conversation]{}, nil
	}
	propertyIDs, err := s.cohosts.ListPropertyIDsWithPermission(ctx, actorID, property.PermReplyMessages)
	if err != nil {
		return shared.PageResult[*message.Conversation]{}, err
	}
	if len(propertyIDs) == 0 {
		return shared.PageResult[*message.Conversation]{}, nil
	}
	return s.messages.ListConversationsByProperties(ctx, propertyIDs, page)
}

// HostUnreadForCohost returns, per conversation in `ids`, the host's unread
// count — what the co-host sees as outstanding work on the team mailbox. The
// caller is assumed to have already been authorised through
// ListCohostConversations (no per-id check here).
func (s *Service) HostUnreadForCohost(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := make(map[uuid.UUID]int64, len(ids))
	for _, id := range ids {
		conv, err := s.messages.FindConversationByID(ctx, id)
		if err != nil {
			return nil, err
		}
		counts, err := s.messages.ConversationUnreadCounts(ctx, conv.HostID)
		if err != nil {
			return nil, err
		}
		if c := counts[id]; c > 0 {
			out[id] = c
		}
	}
	return out, nil
}
