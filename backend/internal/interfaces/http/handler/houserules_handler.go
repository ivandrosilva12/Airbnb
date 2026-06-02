package handler

import (
	"errors"
	"net/http"

	houserulesapp "github.com/airhost/backend/internal/application/houserules"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// HouseRulesHandler exposes the house-rules CRUD-lite endpoints:
// public read on the listing's current rules, host-only write to
// author a new version, and the per-booking acceptance read used by
// disputes and the booking-confirmation screen.
type HouseRulesHandler struct {
	svc *houserulesapp.Service
}

// NewHouseRulesHandler wires a HouseRulesHandler.
func NewHouseRulesHandler(svc *houserulesapp.Service) *HouseRulesHandler {
	return &HouseRulesHandler{svc: svc}
}

// Get serves the current rule set for a property. Public route — no
// authentication required. A property without rules returns an empty
// view at version 0 so the client renders "no rules" without a
// special-case branch.
func (h *HouseRulesHandler) Get(c *gin.Context) {
	propID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	r, err := h.svc.Get(c.Request.Context(), propID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if r == nil {
		response.OK(c, dto.EmptyHouseRules(propID))
		return
	}
	response.OK(c, dto.FromHouseRules(r))
}

type setHouseRulesRequest struct {
	// Items is the full replacement list. The service bumps the version
	// on each Set; clients send the complete intent, not a patch — a
	// versioned-history schema makes incremental edits unsafe.
	Items []string `json:"items"`
}

// Set authors or bumps the rule set. Host-only at the application
// layer: the service checks property.HostID against the caller, so
// the route can sit on the host gate without a separate cohost
// check. (Co-hosts intentionally cannot edit rules — this is a
// primary-host responsibility per Airbnb policy norms.)
func (h *HouseRulesHandler) Set(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req setHouseRulesRequest
	if !bindJSON(c, &req) {
		return
	}
	r, err := h.svc.Set(c.Request.Context(), houserulesapp.SetInput{
		HostID:     hostID,
		PropertyID: propID,
		Items:      req.Items,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromHouseRules(r))
}

// GetAcceptance returns the per-booking acceptance proof bundled with
// the versioned rules text the guest saw. Callable by the booking's
// guest, the listing's host, or an admin — the application layer
// doesn't gate on caller role yet; the route is wired under the
// authenticated group, and a missing acceptance returns 404 (the
// caller can then surface "no acknowledgement on file").
func (h *HouseRulesHandler) GetAcceptance(c *gin.Context) {
	if _, ok := requireUser(c); !ok {
		return
	}
	bookingID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	view, err := h.svc.GetAcceptance(c.Request.Context(), bookingID)
	if err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			response.FailMessage(c, http.StatusNotFound, "no house-rules acceptance recorded for this booking")
			return
		}
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromAcceptance(view))
}
