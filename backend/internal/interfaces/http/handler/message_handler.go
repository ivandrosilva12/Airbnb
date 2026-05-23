package handler

import (
	"net/http"

	messageapp "github.com/airhost/backend/internal/application/message"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

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
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailMessage(c, http.StatusBadRequest, err.Error())
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
	response.Created(c, dto.FromConversation(conv))
}

// ListMine returns the actor's conversations.
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
	items := make([]dto.ConversationView, 0, len(res.Items))
	for _, conv := range res.Items {
		items = append(items, dto.FromConversation(conv))
	}
	response.OK(c, dto.PageView[dto.ConversationView]{Items: items, Total: res.Total})
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
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	m, err := h.svc.SendMessage(c.Request.Context(), actorID, id, req.Body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromMessage(m))
}
