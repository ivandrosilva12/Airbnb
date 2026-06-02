package handler

import (
	"github.com/airhost/backend/internal/application/splitpayment"
	domainsplit "github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// SplitPaymentHandler exposes the per-payer surface for split-payment plans:
// read the plan, authorise your share, list your splits, organizer cancel.
type SplitPaymentHandler struct {
	svc     *splitpaymentapp.Service
	metrics *observability.Metrics
}

// NewSplitPaymentHandler builds a SplitPaymentHandler.
func NewSplitPaymentHandler(svc *splitpaymentapp.Service, m *observability.Metrics) *SplitPaymentHandler {
	return &SplitPaymentHandler{svc: svc, metrics: m}
}

// Get returns the split if the caller is the organizer or one of the payers.
// Used by the per-payer view that shows "your share to pay".
func (h *SplitPaymentHandler) Get(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	sp, err := h.svc.GetByID(c.Request.Context(), actorID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromSplitPayment(sp))
}

// GetForBooking returns the split attached to a booking (organizer/payer only).
func (h *SplitPaymentHandler) GetForBooking(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	bookingID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	sp, err := h.svc.GetByBookingID(c.Request.Context(), actorID, bookingID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromSplitPayment(sp))
}

// AuthorizeShare marks one share paid (in this slice, trust-mode: no real
// gateway capture happens — the payer signing off is the commitment).
func (h *SplitPaymentHandler) AuthorizeShare(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	splitID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	shareID, ok := pathUUID(c, "shareId")
	if !ok {
		return
	}
	sp, err := h.svc.AuthorizeShare(c.Request.Context(), actorID, splitID, shareID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	// S29 — the share authorise call flips the plan to completed when it's
	// the last share. We count the transition (not every authorize) so the
	// metric tracks fully-funded splits 1:1 with the booking confirmations
	// they trigger.
	if h.metrics != nil && sp.Status == domainsplit.StatusCompleted {
		h.metrics.SplitPaymentsCompleted.Inc()
	}
	response.OK(c, dto.FromSplitPayment(sp))
}

// Cancel transitions a pending split to cancelled (organizer only).
func (h *SplitPaymentHandler) Cancel(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	sp, err := h.svc.Cancel(c.Request.Context(), actorID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromSplitPayment(sp))
}

// ListMine returns the splits the caller is organizer or payer in.
func (h *SplitPaymentHandler) ListMine(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.ListMine(c.Request.Context(), actorID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.SplitPaymentView, 0, len(res))
	for _, sp := range res {
		items = append(items, dto.FromSplitPayment(sp))
	}
	response.OK(c, gin.H{"items": items})
}
