package handler

import (
	"net/http"

	pushtokenapp "github.com/airhost/backend/internal/application/pushtoken"
	"github.com/airhost/backend/internal/domain/pushtoken"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// PushTokenHandler exposes endpoints for the mobile/web client to register
// their device for push notifications.
type PushTokenHandler struct {
	svc *pushtokenapp.Service
}

// NewPushTokenHandler builds a PushTokenHandler.
func NewPushTokenHandler(svc *pushtokenapp.Service) *PushTokenHandler {
	return &PushTokenHandler{svc: svc}
}

type registerPushTokenRequest struct {
	Platform string `json:"platform" binding:"required"`
	Token    string `json:"token" binding:"required"`
}

// Register stores a (platform, token) pair for the authenticated user. The
// client should call this on app launch and whenever the OS rotates the token.
func (h *PushTokenHandler) Register(c *gin.Context) {
	uid, ok := requireUser(c)
	if !ok {
		return
	}
	var req registerPushTokenRequest
	if !bindJSON(c, &req) {
		return
	}
	tok, err := h.svc.Register(c.Request.Context(), uid, pushtoken.Platform(req.Platform), req.Token)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromPushToken(tok))
}

type unregisterPushTokenRequest struct {
	Platform string `json:"platform" binding:"required"`
	Token    string `json:"token" binding:"required"`
}

// Unregister removes a (platform, token) pair. Used on logout or token rotation.
func (h *PushTokenHandler) Unregister(c *gin.Context) {
	if _, ok := requireUser(c); !ok {
		return
	}
	var req unregisterPushTokenRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.Unregister(c.Request.Context(), pushtoken.Platform(req.Platform), req.Token); err != nil {
		response.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// List returns every device the authenticated user has registered. Used by the
// settings screen.
func (h *PushTokenHandler) List(c *gin.Context) {
	uid, ok := requireUser(c)
	if !ok {
		return
	}
	tokens, err := h.svc.ListForUser(c.Request.Context(), uid)
	if err != nil {
		response.Fail(c, err)
		return
	}
	views := make([]dto.PushTokenView, 0, len(tokens))
	for _, t := range tokens {
		views = append(views, dto.FromPushToken(t))
	}
	response.OK(c, views)
}
