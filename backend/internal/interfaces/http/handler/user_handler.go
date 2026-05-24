package handler

import (
	"net/http"

	userapp "github.com/airhost/backend/internal/application/user"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// UserHandler exposes user/profile endpoints.
type UserHandler struct {
	svc *userapp.Service
}

// NewUserHandler builds a UserHandler.
func NewUserHandler(svc *userapp.Service) *UserHandler { return &UserHandler{svc: svc} }

// Me returns the authenticated user's profile.
func (h *UserHandler) Me(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.FailMessage(c, http.StatusUnauthorized, "authentication required")
		return
	}
	response.OK(c, dto.FromUser(u))
}

type updateProfileRequest struct {
	FullName  string `json:"fullName" binding:"required"`
	AvatarURL string `json:"avatarUrl"`
}

// UpdateMe updates the authenticated user's profile.
func (h *UserHandler) UpdateMe(c *gin.Context) {
	id, ok := requireUser(c)
	if !ok {
		return
	}
	var req updateProfileRequest
	if !bindJSON(c, &req) {
		return
	}
	u, err := h.svc.UpdateProfile(c.Request.Context(), id, userapp.UpdateProfileInput{
		FullName:  req.FullName,
		AvatarURL: req.AvatarURL,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromUser(u))
}

type emailPreferencesRequest struct {
	Bookings *bool `json:"bookings"`
	Messages *bool `json:"messages"`
}

// UpdatePreferences sets the authenticated user's transactional email opt-ins.
// Omitted fields keep their current value.
func (h *UserHandler) UpdatePreferences(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.FailMessage(c, http.StatusUnauthorized, "authentication required")
		return
	}
	var req emailPreferencesRequest
	if !bindJSON(c, &req) {
		return
	}
	prefs := u.EmailPrefs
	if req.Bookings != nil {
		prefs.Bookings = *req.Bookings
	}
	if req.Messages != nil {
		prefs.Messages = *req.Messages
	}
	updated, err := h.svc.UpdateEmailPreferences(c.Request.Context(), u.ID, prefs)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromUser(updated))
}

// BecomeHost promotes the authenticated user to host.
func (h *UserHandler) BecomeHost(c *gin.Context) {
	id, ok := requireUser(c)
	if !ok {
		return
	}
	u, err := h.svc.BecomeHost(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromUser(u))
}
