package handler

import (
	reportapp "github.com/airhost/backend/internal/application/report"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ReportHandler exposes listing-report endpoints: filing a report (any
// authenticated user) and the administrator moderation queue.
type ReportHandler struct {
	svc *reportapp.Service
}

// NewReportHandler builds a ReportHandler.
func NewReportHandler(svc *reportapp.Service) *ReportHandler {
	return &ReportHandler{svc: svc}
}

type createReportRequest struct {
	Reason string `json:"reason" binding:"required"`
	Note   string `json:"note"`
}

// Create files a report against a listing.
func (h *ReportHandler) Create(c *gin.Context) {
	reporterID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req createReportRequest
	if !bindJSON(c, &req) {
		return
	}
	r, err := h.svc.File(c.Request.Context(), reporterID, propertyID, req.Reason, req.Note)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromReport(r))
}

// ListOpen returns the administrator moderation queue.
func (h *ReportHandler) ListOpen(c *gin.Context) {
	res, err := h.svc.ListOpen(c.Request.Context(), pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.ReportView, 0, len(res.Items))
	for _, e := range res.Items {
		items = append(items, dto.FromEnrichedReport(e))
	}
	response.OK(c, dto.PageView[dto.ReportView]{Items: items, Total: res.Total})
}

type resolveReportRequest struct {
	Resolution string `json:"resolution"`
}

// Resolve marks an open report acted-upon (administrator action).
func (h *ReportHandler) Resolve(c *gin.Context) {
	adminID, id, req, ok := h.bindDecide(c)
	if !ok {
		return
	}
	r, err := h.svc.Resolve(c.Request.Context(), adminID, id, req.Resolution)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromReport(r))
}

// Dismiss marks an open report as requiring no action (administrator action).
func (h *ReportHandler) Dismiss(c *gin.Context) {
	adminID, id, req, ok := h.bindDecide(c)
	if !ok {
		return
	}
	r, err := h.svc.Dismiss(c.Request.Context(), adminID, id, req.Resolution)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromReport(r))
}

// bindDecide pulls the admin, the report id and the (optional) resolution note
// shared by Resolve and Dismiss.
func (h *ReportHandler) bindDecide(c *gin.Context) (adminID, reportID uuid.UUID, req resolveReportRequest, ok bool) {
	adminID, ok = requireUser(c)
	if !ok {
		return
	}
	reportID, ok = pathUUID(c, "id")
	if !ok {
		return
	}
	// Resolution note is optional; ignore body-bind errors for an empty body.
	_ = c.ShouldBindJSON(&req)
	return adminID, reportID, req, true
}
