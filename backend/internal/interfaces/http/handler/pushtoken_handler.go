package handler

import (
	"encoding/json"
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
	svc             *pushtokenapp.Service
	vapidPublicKey  string
}

// NewPushTokenHandler builds a PushTokenHandler.
func NewPushTokenHandler(svc *pushtokenapp.Service) *PushTokenHandler {
	return &PushTokenHandler{svc: svc}
}

// WithVAPIDPublicKey exposes the public VAPID key the browser passes to
// pushManager.subscribe. An empty key surfaces as 503 from the endpoint so
// the client can show a clean "web push not configured on this server" state.
func (h *PushTokenHandler) WithVAPIDPublicKey(key string) *PushTokenHandler {
	h.vapidPublicKey = key
	return h
}

// VAPIDPublicKey returns the public key as plain text so the browser can
// decode the URL-safe base64 string directly with `urlBase64ToUint8Array`.
// Open endpoint (no auth) so the SPA can fetch it before the user signs in.
func (h *PushTokenHandler) VAPIDPublicKey(c *gin.Context) {
	if h.vapidPublicKey == "" {
		c.String(http.StatusServiceUnavailable, "web push is not configured")
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.String(http.StatusOK, h.vapidPublicKey)
}

type registerPushTokenRequest struct {
	Platform string `json:"platform" binding:"required"`
	// Token carries the FCM/APNs token for native devices, or the Push
	// Service subscription endpoint URL for web. Either is mandatory.
	Token string `json:"token" binding:"required"`
	// Keys is the Web Push subscription's {p256dh, auth} pair. Only
	// meaningful when platform=="web"; ignored otherwise so the FCM/APNs
	// register paths stay byte-compatible with the existing mobile clients.
	Keys *webPushKeysDTO `json:"keys,omitempty"`
}

// webPushKeysDTO mirrors the JSON the browser produces via
// `subscription.toJSON().keys` so the client can POST it verbatim.
type webPushKeysDTO struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
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
	platform := pushtoken.Platform(req.Platform)
	var endpoint string
	if platform == pushtoken.PlatformWeb && req.Keys != nil {
		blob, err := json.Marshal(req.Keys)
		if err != nil {
			response.Fail(c, err)
			return
		}
		endpoint = string(blob)
	}
	tok, err := h.svc.Register(c.Request.Context(), uid, platform, req.Token, endpoint)
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
