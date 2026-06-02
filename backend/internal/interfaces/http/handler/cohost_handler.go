package handler

import (
	"context"
	"errors"
	"net/http"

	propertyapp "github.com/airhost/backend/internal/application/property"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// userLookup is the subset of the user repository the cohost handler needs to
// hydrate display info on returned grants. Defined as a local interface to
// keep the handler decoupled from infrastructure types.
type userLookup interface {
	FindByID(ctx context.Context, id uuid.UUID) (*user.User, error)
}

// CohostHandler exposes the host-only endpoints for managing per-listing
// co-host grants, plus the read-only "listings I help manage" endpoint
// reachable by any authenticated user.
type CohostHandler struct {
	svc     *propertyapp.CohostService
	users   userLookup
	metrics *observability.Metrics
}

// NewCohostHandler wires a CohostHandler.
func NewCohostHandler(svc *propertyapp.CohostService, users userLookup, m *observability.Metrics) *CohostHandler {
	return &CohostHandler{svc: svc, users: users, metrics: m}
}

// inviteRequest accepts either an explicit user id or an email to look up.
type inviteRequest struct {
	UserID      string   `json:"userId"`
	Email       string   `json:"email"`
	Permissions []string `json:"permissions" binding:"required"`
}

// updatePermissionsRequest replaces a grant's permission set.
type updatePermissionsRequest struct {
	Permissions []string `json:"permissions" binding:"required"`
}

func toCohostPermissions(in []string) []property.CohostPermission {
	out := make([]property.CohostPermission, 0, len(in))
	for _, raw := range in {
		out = append(out, property.CohostPermission(raw))
	}
	return out
}

// List returns the co-hosts on a listing the caller owns.
func (h *CohostHandler) List(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	cohosts, err := h.svc.ListForProperty(c.Request.Context(), hostID, propertyID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.CohostView, 0, len(cohosts))
	for _, ch := range cohosts {
		u, _ := h.users.FindByID(c.Request.Context(), ch.UserID) // best-effort hydration
		items = append(items, dto.FromCohost(ch, u))
	}
	response.OK(c, gin.H{"items": items})
}

// Invite grants a user one or more permissions on a listing the caller owns.
func (h *CohostHandler) Invite(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req inviteRequest
	if !bindJSON(c, &req) {
		return
	}
	var userID uuid.UUID
	if req.UserID != "" {
		parsed, err := uuid.Parse(req.UserID)
		if err != nil {
			response.FailMessage(c, http.StatusBadRequest, "invalid userId")
			return
		}
		userID = parsed
	}
	cohost, err := h.svc.Invite(c.Request.Context(), propertyapp.InviteInput{
		HostID:      hostID,
		PropertyID:  propertyID,
		UserID:      userID,
		Email:       req.Email,
		Permissions: toCohostPermissions(req.Permissions),
	})
	if err != nil {
		// A duplicate grant maps to 409 so the UI can suggest "update instead".
		if errors.Is(err, shared.ErrConflict) {
			response.FailMessage(c, http.StatusConflict, "that user is already a co-host on this listing")
			return
		}
		response.Fail(c, err)
		return
	}
	if h.metrics != nil {
		h.metrics.CohostActions.WithLabelValues("invited").Inc()
	}
	u, _ := h.users.FindByID(c.Request.Context(), cohost.UserID)
	response.Created(c, dto.FromCohost(cohost, u))
}

// UpdatePermissions replaces a grant's permission set.
func (h *CohostHandler) UpdatePermissions(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	cohostID, ok := pathUUID(c, "cohostId")
	if !ok {
		return
	}
	var req updatePermissionsRequest
	if !bindJSON(c, &req) {
		return
	}
	cohost, err := h.svc.SetPermissions(
		c.Request.Context(), hostID, propertyID, cohostID, toCohostPermissions(req.Permissions),
	)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if h.metrics != nil {
		h.metrics.CohostActions.WithLabelValues("permissions_changed").Inc()
	}
	u, _ := h.users.FindByID(c.Request.Context(), cohost.UserID)
	response.OK(c, dto.FromCohost(cohost, u))
}

// Revoke deletes a grant.
func (h *CohostHandler) Revoke(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	cohostID, ok := pathUUID(c, "cohostId")
	if !ok {
		return
	}
	if err := h.svc.Revoke(c.Request.Context(), hostID, propertyID, cohostID); err != nil {
		response.Fail(c, err)
		return
	}
	if h.metrics != nil {
		h.metrics.CohostActions.WithLabelValues("revoked").Inc()
	}
	response.NoContent(c)
}

// ListMine returns the listings on which the caller is a co-host (not the
// primary host). The caller need not have the `host` role.
func (h *CohostHandler) ListMine(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	listings, err := h.svc.ListMine(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.PropertyView, 0, len(listings))
	for _, p := range listings {
		// These are listings the caller can act on as a co-host, so they
		// see the host-only arrival block too (the same fields the primary
		// host edits and that the co-host may need to coordinate arrivals).
		items = append(items, dto.FromPropertyForHost(p))
	}
	response.OK(c, gin.H{"items": items})
}
