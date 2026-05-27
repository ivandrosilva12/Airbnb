package handler

import (
	savedsearchapp "github.com/airhost/backend/internal/application/savedsearch"
	"github.com/airhost/backend/internal/domain/savedsearch"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// SavedSearchHandler exposes saved-search endpoints: save the current search,
// list, delete, and toggle new-listing alerts.
type SavedSearchHandler struct {
	svc *savedsearchapp.Service
}

// NewSavedSearchHandler builds a SavedSearchHandler.
func NewSavedSearchHandler(svc *savedsearchapp.Service) *SavedSearchHandler {
	return &SavedSearchHandler{svc: svc}
}

func savedSearchView(s *savedsearch.SavedSearch) gin.H {
	return gin.H{
		"id":            s.ID,
		"name":          s.Name,
		"query":         s.Query,
		"alertsEnabled": s.AlertsEnabled,
		"createdAt":     s.CreatedAt,
	}
}

type createSavedSearchRequest struct {
	Name          string `json:"name" binding:"required"`
	Query         string `json:"query"`
	AlertsEnabled bool   `json:"alertsEnabled"`
}

// Create saves the user's current search.
func (h *SavedSearchHandler) Create(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	var req createSavedSearchRequest
	if !bindJSON(c, &req) {
		return
	}
	s, err := h.svc.Save(c.Request.Context(), userID, req.Name, req.Query, req.AlertsEnabled)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, savedSearchView(s))
}

// List returns the user's saved searches.
func (h *SavedSearchHandler) List(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	searches, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(searches))
	for _, s := range searches {
		items = append(items, savedSearchView(s))
	}
	response.OK(c, gin.H{"items": items})
}

// Delete removes a saved search the user owns.
func (h *SavedSearchHandler) Delete(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), userID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

type setAlertsRequest struct {
	AlertsEnabled bool `json:"alertsEnabled"`
}

// SetAlerts toggles new-listing alerts for a saved search.
func (h *SavedSearchHandler) SetAlerts(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req setAlertsRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.SetAlerts(c.Request.Context(), userID, id, req.AlertsEnabled); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}
