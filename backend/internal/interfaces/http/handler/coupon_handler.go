package handler

import (
	"net/http"
	"time"

	auditapp "github.com/airhost/backend/internal/application/audit"
	couponapp "github.com/airhost/backend/internal/application/coupon"
	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// CouponHandler exposes admin promo-code management endpoints.
// audit is optional (S54) — wired at the composition root via
// WithAudit. When nil, Deactivate still works but no trail row is
// written, matching the pattern in PropertyHandler / IdentityHandler /
// DisputeHandler / ReportHandler.
type CouponHandler struct {
	svc   *couponapp.Service
	audit *auditapp.Service
}

// NewCouponHandler builds a CouponHandler.
func NewCouponHandler(svc *couponapp.Service) *CouponHandler {
	return &CouponHandler{svc: svc}
}

// WithAudit attaches the audit service so Deactivate writes a row.
// Returns the receiver for chaining.
func (h *CouponHandler) WithAudit(a *auditapp.Service) *CouponHandler {
	h.audit = a
	return h
}

type createCouponRequest struct {
	Code           string  `json:"code" binding:"required"`
	Kind           string  `json:"kind" binding:"required"`
	Percent        float64 `json:"percent"`
	AmountCents    int64   `json:"amountCents"`
	Currency       string  `json:"currency"`
	MinNights      int     `json:"minNights"`
	MaxRedemptions int     `json:"maxRedemptions"`
	ExpiresAt      string  `json:"expiresAt"` // optional RFC3339; empty = never
}

// Create mints a coupon.
func (h *CouponHandler) Create(c *gin.Context) {
	var req createCouponRequest
	if !bindJSON(c, &req) {
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			response.FailMessage(c, http.StatusBadRequest, "expiresAt must be an RFC3339 timestamp")
			return
		}
		expiresAt = &t
	}
	cp, err := h.svc.Create(c.Request.Context(), couponapp.CreateInput{
		Code:           req.Code,
		Kind:           req.Kind,
		Percent:        req.Percent,
		AmountCents:    req.AmountCents,
		Currency:       req.Currency,
		MinNights:      req.MinNights,
		MaxRedemptions: req.MaxRedemptions,
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromCoupon(cp))
}

// List returns all coupons (most recent first).
func (h *CouponHandler) List(c *gin.Context) {
	res, err := h.svc.List(c.Request.Context(), pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.CouponView, 0, len(res.Items))
	for _, cp := range res.Items {
		items = append(items, dto.FromCoupon(cp))
	}
	response.OK(c, dto.PageView[dto.CouponView]{Items: items, Total: res.Total})
}

// Deactivate disables a coupon. Records an audit row on success
// (S54): action=coupon.deactivate, target=coupon:<id>,
// metadata.code carries the code so a later read knows which one
// without joining back to the coupon table.
func (h *CouponHandler) Deactivate(c *gin.Context) {
	adminID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	cp, err := h.svc.Deactivate(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if h.audit != nil {
		if err := h.audit.Record(c.Request.Context(), auditapp.RecordInput{
			ActorID: adminID, Action: audit.ActionCouponDeactivate,
			TargetType: audit.TargetCoupon, TargetID: id,
			Metadata: map[string]any{"code": cp.Code},
		}); err != nil {
			response.Fail(c, err)
			return
		}
	}
	response.OK(c, dto.FromCoupon(cp))
}
