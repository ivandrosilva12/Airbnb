package handler

import (
	"errors"
	"io"
	"net/http"

	messageapp "github.com/airhost/backend/internal/application/message"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxAttachmentBytes = 10 << 20 // 10 MiB

// allowedAttachmentContentTypes are the file types a chat attachment may have:
// the same images permitted for listing photos, plus PDF documents.
var allowedAttachmentContentTypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"image/gif":       true,
	"application/pdf": true,
}

// MessageHandler exposes host↔guest messaging endpoints.
type MessageHandler struct {
	svc *messageapp.Service
}

// NewMessageHandler builds a MessageHandler.
func NewMessageHandler(svc *messageapp.Service) *MessageHandler { return &MessageHandler{svc: svc} }

type startConversationRequest struct {
	PropertyID string `json:"propertyId" binding:"required"`
}

// Start creates or returns the thread between the guest and a property's host.
func (h *MessageHandler) Start(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	var req startConversationRequest
	if !bindJSON(c, &req) {
		return
	}
	propertyID, ok := parseUUID(c, req.PropertyID, "propertyId")
	if !ok {
		return
	}
	conv, err := h.svc.StartConversation(c.Request.Context(), actorID, propertyID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromConversation(conv, 0))
}

// ListCohostMailbox returns the conversations on listings the caller helps
// manage as a co-host with reply_messages. Each entry carries the host-side
// unread count — what the team still has to handle.
func (h *MessageHandler) ListCohostMailbox(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.ListCohostConversations(c.Request.Context(), actorID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	ids := make([]uuid.UUID, 0, len(res.Items))
	for _, conv := range res.Items {
		ids = append(ids, conv.ID)
	}
	unread, err := h.svc.HostUnreadForCohost(c.Request.Context(), ids)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.ConversationView, 0, len(res.Items))
	for _, conv := range res.Items {
		items = append(items, dto.FromConversation(conv, unread[conv.ID]))
	}
	response.OK(c, dto.PageView[dto.ConversationView]{Items: items, Total: res.Total})
}

// ListMine returns the actor's conversations, each with their unread count.
func (h *MessageHandler) ListMine(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.ListConversations(c.Request.Context(), actorID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	unread, err := h.svc.UnreadByConversation(c.Request.Context(), actorID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.ConversationView, 0, len(res.Items))
	for _, conv := range res.Items {
		items = append(items, dto.FromConversation(conv, unread[conv.ID]))
	}
	response.OK(c, dto.PageView[dto.ConversationView]{Items: items, Total: res.Total})
}

// UnreadCount returns the actor's total unread message count.
func (h *MessageHandler) UnreadCount(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	total, err := h.svc.TotalUnread(c.Request.Context(), actorID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"unread": total})
}

// MarkRead marks a conversation read for the actor.
func (h *MessageHandler) MarkRead(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.MarkRead(c.Request.Context(), actorID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// ListMessages returns the messages of a conversation the actor takes part in.
func (h *MessageHandler) ListMessages(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	res, err := h.svc.ListMessages(c.Request.Context(), actorID, id, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.MessageView, 0, len(res.Items))
	for _, m := range res.Items {
		items = append(items, dto.FromMessage(m))
	}
	response.OK(c, dto.PageView[dto.MessageView]{Items: items, Total: res.Total})
}

type sendMessageRequest struct {
	Body string `json:"body" binding:"required"`
}

// Send posts a message to a conversation.
func (h *MessageHandler) Send(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req sendMessageRequest
	if !bindJSON(c, &req) {
		return
	}
	m, err := h.svc.SendMessage(c.Request.Context(), actorID, id, req.Body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromMessage(m))
}

// SendAttachment posts a file (image or PDF) — with an optional caption — to a
// conversation. The file rides in a multipart "file" field; the caption, if
// any, in a "body" field.
func (h *MessageHandler) SendAttachment(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	// Cap the request body so an oversized upload is rejected while parsing.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAttachmentBytes+1<<10)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.FailMessage(c, http.StatusRequestEntityTooLarge, "attachment exceeds the 10 MiB limit")
			return
		}
		response.FailMessage(c, http.StatusBadRequest, "attachment file is required")
		return
	}
	if fileHeader.Size > maxAttachmentBytes {
		response.FailMessage(c, http.StatusRequestEntityTooLarge, "attachment exceeds the 10 MiB limit")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "could not read uploaded file")
		return
	}
	defer file.Close()

	// Detect the content type from the leading bytes rather than trusting the
	// client header, then rewind so the whole file is stored.
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	contentType := http.DetectContentType(head[:n])
	if !allowedAttachmentContentTypes[contentType] {
		response.FailMessage(c, http.StatusUnsupportedMediaType, "only JPEG, PNG, WebP, GIF or PDF files are allowed")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		response.FailMessage(c, http.StatusBadRequest, "could not read uploaded file")
		return
	}

	m, err := h.svc.SendAttachment(c.Request.Context(), actorID, id, messageapp.AttachmentInput{
		Body:        c.PostForm("body"),
		Reader:      file,
		Size:        fileHeader.Size,
		ContentType: contentType,
		Filename:    fileHeader.Filename,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromMessage(m))
}
