package handler

import (
	userblockapp "github.com/airhost/backend/internal/application/userblock"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// UserBlockHandler exposes endpoints for blocking and unblocking users.
type UserBlockHandler struct {
	svc *userblockapp.Service
}

// NewUserBlockHandler builds a UserBlockHandler.
func NewUserBlockHandler(svc *userblockapp.Service) *UserBlockHandler {
	return &UserBlockHandler{svc: svc}
}

// Block blocks the user identified by the path id for the authenticated user.
func (h *UserBlockHandler) Block(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	blockedID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Block(c.Request.Context(), actorID, blockedID); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// Unblock lifts a block the authenticated user placed on the path id.
func (h *UserBlockHandler) Unblock(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	blockedID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Unblock(c.Request.Context(), actorID, blockedID); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// ListBlocked returns the IDs of users the authenticated user has blocked.
func (h *UserBlockHandler) ListBlocked(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	ids, err := h.svc.ListBlocked(c.Request.Context(), actorID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	response.OK(c, gin.H{"blocked": ids})
}
