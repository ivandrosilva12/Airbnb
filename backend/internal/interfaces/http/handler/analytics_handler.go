package handler

import (
	analyticsapp "github.com/airhost/backend/internal/application/analytics"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// AnalyticsHandler exposes host reporting endpoints.
type AnalyticsHandler struct {
	svc *analyticsapp.Service
}

// NewAnalyticsHandler builds an AnalyticsHandler.
func NewAnalyticsHandler(svc *analyticsapp.Service) *AnalyticsHandler {
	return &AnalyticsHandler{svc: svc}
}

// HostMetrics returns the authenticated host's dashboard analytics.
func (h *AnalyticsHandler) HostMetrics(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	m, err := h.svc.HostMetrics(c.Request.Context(), hostID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromHostMetrics(m))
}
