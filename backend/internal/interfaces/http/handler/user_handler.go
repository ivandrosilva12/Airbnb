package handler

import (
	"net/http"

	auditapp "github.com/airhost/backend/internal/application/audit"
	userapp "github.com/airhost/backend/internal/application/user"
	"github.com/airhost/backend/internal/domain/audit"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// UserHandler exposes user/profile endpoints.
type UserHandler struct {
	svc   *userapp.Service
	audit *auditapp.Service // nil = audit recording disabled
}

// NewUserHandler builds a UserHandler.
func NewUserHandler(svc *userapp.Service) *UserHandler { return &UserHandler{svc: svc} }

// WithAudit attaches an audit-recording service (S61). When set, the admin
// suspend/unsuspend handlers append an audit row on success; failing to
// audit is a hard error so we don't ship a state change without a trail.
func (h *UserHandler) WithAudit(a *auditapp.Service) *UserHandler {
	h.audit = a
	return h
}

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

type preferenceToggles struct {
	Bookings *bool `json:"bookings"`
	Messages *bool `json:"messages"`
}

// preferencesRequest carries optional email and push opt-in toggles. Top-level
// "bookings"/"messages" remain accepted for backwards compatibility with
// existing clients (they mutate the email side only); newer clients should send
// nested "email" / "push" objects.
type preferencesRequest struct {
	Bookings *bool              `json:"bookings"`
	Messages *bool              `json:"messages"`
	Email    *preferenceToggles `json:"email"`
	Push     *preferenceToggles `json:"push"`
}

// UpdatePreferences sets the authenticated user's notification opt-ins. Email
// and push are tracked independently; omitted fields keep their current value.
func (h *UserHandler) UpdatePreferences(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.FailMessage(c, http.StatusUnauthorized, "authentication required")
		return
	}
	var req preferencesRequest
	if !bindJSON(c, &req) {
		return
	}
	// Apply email toggles (top-level + nested).
	emailPrefs := u.EmailPrefs
	if req.Bookings != nil {
		emailPrefs.Bookings = *req.Bookings
	}
	if req.Messages != nil {
		emailPrefs.Messages = *req.Messages
	}
	if req.Email != nil {
		if req.Email.Bookings != nil {
			emailPrefs.Bookings = *req.Email.Bookings
		}
		if req.Email.Messages != nil {
			emailPrefs.Messages = *req.Email.Messages
		}
	}
	updated, err := h.svc.UpdateEmailPreferences(c.Request.Context(), u.ID, emailPrefs)
	if err != nil {
		response.Fail(c, err)
		return
	}
	// Apply push toggles (only the nested "push" path accepts them).
	if req.Push != nil {
		pushPrefs := updated.PushPrefs
		if req.Push.Bookings != nil {
			pushPrefs.Bookings = *req.Push.Bookings
		}
		if req.Push.Messages != nil {
			pushPrefs.Messages = *req.Push.Messages
		}
		updated, err = h.svc.UpdatePushPreferences(c.Request.Context(), u.ID, domainuser.PushPreferences{
			Bookings: pushPrefs.Bookings,
			Messages: pushPrefs.Messages,
		})
		if err != nil {
			response.Fail(c, err)
			return
		}
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

// AdminSuspend locks a user out of the platform (S61). The auth middleware
// already refuses inactive bearers, so flipping IsActive false here is the
// minimal mechanism — no token revocation needed. Records an audit row on
// success; failure to audit is a hard error so we don't strip a user of
// access without a trail.
func (h *UserHandler) AdminSuspend(c *gin.Context) {
	adminID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if id == adminID {
		response.FailMessage(c, http.StatusBadRequest, "an admin cannot suspend themselves")
		return
	}
	u, err := h.svc.Suspend(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if h.audit != nil {
		if err := h.audit.Record(c.Request.Context(), auditapp.RecordInput{
			ActorID: adminID, Action: audit.ActionUserSuspend,
			TargetType: audit.TargetUser, TargetID: id,
			Metadata: map[string]any{"email": u.Email},
		}); err != nil {
			response.Fail(c, err)
			return
		}
	}
	response.OK(c, dto.FromUser(u))
}

// AdminUnsuspend reinstates a previously suspended user (S61). Anonymised
// accounts (GDPR-erased) cannot be reactivated; the service returns
// ErrValidation in that case.
func (h *UserHandler) AdminUnsuspend(c *gin.Context) {
	adminID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	u, err := h.svc.Unsuspend(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if h.audit != nil {
		if err := h.audit.Record(c.Request.Context(), auditapp.RecordInput{
			ActorID: adminID, Action: audit.ActionUserUnsuspend,
			TargetType: audit.TargetUser, TargetID: id,
			Metadata: map[string]any{"email": u.Email},
		}); err != nil {
			response.Fail(c, err)
			return
		}
	}
	response.OK(c, dto.FromUser(u))
}
