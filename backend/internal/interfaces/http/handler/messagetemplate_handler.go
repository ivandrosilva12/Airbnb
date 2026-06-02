package handler

import (
	"github.com/airhost/backend/internal/application/messagetemplate"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// MessageTemplateHandler exposes the host's saved-reply playbook endpoints.
type MessageTemplateHandler struct {
	svc *messagetemplateapp.Service
}

// NewMessageTemplateHandler builds a MessageTemplateHandler.
func NewMessageTemplateHandler(svc *messagetemplateapp.Service) *MessageTemplateHandler {
	return &MessageTemplateHandler{svc: svc}
}

type messageTemplateRequest struct {
	Label string `json:"label" binding:"required"`
	Body  string `json:"body" binding:"required"`
}

// List returns the caller's templates sorted by label.
func (h *MessageTemplateHandler) List(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.ListMine(c.Request.Context(), actorID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.MessageTemplateView, 0, len(res))
	for _, t := range res {
		items = append(items, dto.FromMessageTemplate(t))
	}
	response.OK(c, gin.H{"items": items})
}

// Create persists a new template for the caller.
func (h *MessageTemplateHandler) Create(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	var req messageTemplateRequest
	if !bindJSON(c, &req) {
		return
	}
	t, err := h.svc.Create(c.Request.Context(), actorID, req.Label, req.Body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromMessageTemplate(t))
}

// Update replaces the label/body on a template the caller owns.
func (h *MessageTemplateHandler) Update(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req messageTemplateRequest
	if !bindJSON(c, &req) {
		return
	}
	t, err := h.svc.Update(c.Request.Context(), actorID, id, req.Label, req.Body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromMessageTemplate(t))
}

// Delete removes a template the caller owns.
func (h *MessageTemplateHandler) Delete(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), actorID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}
