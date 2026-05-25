package handler

import (
	"net/http"
	"time"

	couponapp "github.com/airhost/backend/internal/application/coupon"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// CouponHandler exposes admin promo-code management endpoints.
type CouponHandler struct {
	svc *couponapp.Service
}

// NewCouponHandler builds a CouponHandler.
func NewCouponHandler(svc *couponapp.Service) *CouponHandler {
	return &CouponHandler{svc: svc}
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

// Deactivate disables a coupon.
func (h *CouponHandler) Deactivate(c *gin.Context) {
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	cp, err := h.svc.Deactivate(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromCoupon(cp))
}
