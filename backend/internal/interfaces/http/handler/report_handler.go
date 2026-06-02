package handler

import (
	auditapp "github.com/airhost/backend/internal/application/audit"
	reportapp "github.com/airhost/backend/internal/application/report"
	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ReportHandler exposes listing-report endpoints: filing a report (any
// authenticated user) and the administrator moderation queue.
//
// audit is optional (S54) — wired via WithAudit at the composition
// root. When nil the admin Resolve/Dismiss paths still work but
// don't write an audit row, matching the early-tests pattern shared
// with PropertyHandler / IdentityHandler / DisputeHandler.
type ReportHandler struct {
	svc   *reportapp.Service
	audit *auditapp.Service
}

// NewReportHandler builds a ReportHandler.
func NewReportHandler(svc *reportapp.Service) *ReportHandler {
	return &ReportHandler{svc: svc}
}

// WithAudit attaches the audit service so admin Resolve/Dismiss
// record a trail (S54). Returns the receiver for chaining.
func (h *ReportHandler) WithAudit(a *auditapp.Service) *ReportHandler {
	h.audit = a
	return h
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

// CreateForReview files a report against a review.
func (h *ReportHandler) CreateForReview(c *gin.Context) {
	reporterID, ok := requireUser(c)
	if !ok {
		return
	}
	reviewID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req createReportRequest
	if !bindJSON(c, &req) {
		return
	}
	r, err := h.svc.FileReviewReport(c.Request.Context(), reporterID, reviewID, req.Reason, req.Note)
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
// Records an audit row on success (S54): action=report.resolve,
// target=report:<id>, metadata.resolution carries the moderator's
// note. Audit failure is a HARD error — same policy as the other
// admin handlers (S45): we don't ship a state change without a trail.
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
	if h.audit != nil {
		if err := h.audit.Record(c.Request.Context(), auditapp.RecordInput{
			ActorID: adminID, Action: audit.ActionReportResolve,
			TargetType: audit.TargetReport, TargetID: id,
			Metadata: map[string]any{"resolution": req.Resolution},
		}); err != nil {
			response.Fail(c, err)
			return
		}
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
	if h.audit != nil {
		if err := h.audit.Record(c.Request.Context(), auditapp.RecordInput{
			ActorID: adminID, Action: audit.ActionReportDismiss,
			TargetType: audit.TargetReport, TargetID: id,
			Metadata: map[string]any{"resolution": req.Resolution},
		}); err != nil {
			response.Fail(c, err)
			return
		}
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
