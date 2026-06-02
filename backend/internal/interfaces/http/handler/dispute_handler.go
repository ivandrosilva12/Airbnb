package handler

import (
	"sort"

	disputeapp "github.com/airhost/backend/internal/application/dispute"
	domaindispute "github.com/airhost/backend/internal/domain/dispute"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// DisputeHandler exposes Resolution Center endpoints: guests/hosts open and
// participate in cases, admins decide them.
type DisputeHandler struct {
	svc     *disputeapp.Service
	metrics *observability.Metrics
}

// NewDisputeHandler builds a DisputeHandler.
func NewDisputeHandler(svc *disputeapp.Service, m *observability.Metrics) *DisputeHandler {
	return &DisputeHandler{svc: svc, metrics: m}
}

type openDisputeRequest struct {
	Kind                 string `json:"kind" binding:"required"`
	Reason               string `json:"reason" binding:"required"`
	RequestedAmountCents int64  `json:"requestedAmountCents"`
	Currency             string `json:"currency"`
}

// Open files a new dispute on the booking in the URL.
func (h *DisputeHandler) Open(c *gin.Context) {
	uid, ok := requireUser(c)
	if !ok {
		return
	}
	bookingID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req openDisputeRequest
	if !bindJSON(c, &req) {
		return
	}
	d, err := h.svc.Open(c.Request.Context(), disputeapp.OpenInput{
		BookingID:            bookingID,
		OpenerID:             uid,
		Kind:                 domaindispute.Kind(req.Kind),
		Reason:               req.Reason,
		RequestedAmountCents: req.RequestedAmountCents,
		Currency:             req.Currency,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	if h.metrics != nil {
		h.metrics.DisputesOpened.Inc()
	}
	response.Created(c, dto.FromDispute(d))
}

// ListMine returns disputes the caller opened or hosts at.
func (h *DisputeHandler) ListMine(c *gin.Context) {
	uid, ok := requireUser(c)
	if !ok {
		return
	}
	items, err := h.svc.ListMine(c.Request.Context(), uid, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]dto.DisputeView, 0, len(items))
	for _, d := range items {
		out = append(out, dto.FromDispute(d))
	}
	response.OK(c, out)
}

// Get returns one dispute the caller is allowed to see.
func (h *DisputeHandler) Get(c *gin.Context) {
	uid, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	d, err := h.svc.GetByID(c.Request.Context(), uid, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromDispute(d))
}

type evidenceRequest struct {
	URL  string `json:"url"`
	Note string `json:"note"`
}

// AddEvidence appends a piece of supporting material to a dispute.
func (h *DisputeHandler) AddEvidence(c *gin.Context) {
	uid, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req evidenceRequest
	if !bindJSON(c, &req) {
		return
	}
	d, err := h.svc.AddEvidence(c.Request.Context(), uid, id, req.URL, req.Note)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromDispute(d))
}

type hostResponseRequest struct {
	Response string `json:"response" binding:"required"`
}

// HostRespond records the host's side. Host-only.
func (h *DisputeHandler) HostRespond(c *gin.Context) {
	uid, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req hostResponseRequest
	if !bindJSON(c, &req) {
		return
	}
	d, err := h.svc.HostRespond(c.Request.Context(), uid, id, req.Response)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromDispute(d))
}

// AdminListOpen returns the moderation queue, sorted so overdue cases
// (past the SLA deadline) bubble to the top in oldest-overdue-first order.
// The repo already returns the open set; the SLA re-sort is a transport
// concern (kept here to avoid plumbing an SLA-aware ListOpen variant
// through the application layer for the same data).
func (h *DisputeHandler) AdminListOpen(c *gin.Context) {
	if _, ok := requireUser(c); !ok {
		return
	}
	res, err := h.svc.ListOpen(c.Request.Context(), pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]dto.DisputeView, 0, len(res.Items))
	for _, d := range res.Items {
		out = append(out, dto.FromDispute(d))
	}
	// Stable sort: overdue first; within each bucket, oldest OpenedAt first
	// (so the case that's been waiting longest is at the top).
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Overdue != out[j].Overdue {
			return out[i].Overdue
		}
		return out[i].OpenedAt.Before(out[j].OpenedAt)
	})
	response.OK(c, gin.H{"items": out, "total": res.Total})
}

type adminDecisionRequest struct {
	Resolution        string `json:"resolution" binding:"required"`
	RefundAmountCents int64  `json:"refundAmountCents"`
	DamageAmountCents int64  `json:"damageAmountCents"`
}

// AdminResolve closes a dispute siding with the opener.
func (h *DisputeHandler) AdminResolve(c *gin.Context) { h.adminDecide(c, true) }

// AdminReject closes a dispute siding against the opener.
func (h *DisputeHandler) AdminReject(c *gin.Context) { h.adminDecide(c, false) }

func (h *DisputeHandler) adminDecide(c *gin.Context, sideWithOpener bool) {
	uid, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req adminDecisionRequest
	if !bindJSON(c, &req) {
		return
	}
	in := disputeapp.ResolveInput{
		AdminID:           uid,
		DisputeID:         id,
		Resolution:        req.Resolution,
		RefundAmountCents: req.RefundAmountCents,
		DamageAmountCents: req.DamageAmountCents,
	}
	var (
		d   *domaindispute.Dispute
		err error
	)
	if sideWithOpener {
		d, err = h.svc.AdminResolve(c.Request.Context(), in)
	} else {
		d, err = h.svc.AdminReject(c.Request.Context(), in)
	}
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromDispute(d))
}
