package handler

import (
	"net/http"
	"time"

	priceruleapp "github.com/airhost/backend/internal/application/pricerule"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// PriceRuleHandler exposes seasonal/per-date pricing endpoints scoped to a
// listing the host owns.
type PriceRuleHandler struct {
	svc *priceruleapp.Service
}

// NewPriceRuleHandler builds a PriceRuleHandler.
func NewPriceRuleHandler(svc *priceruleapp.Service) *PriceRuleHandler {
	return &PriceRuleHandler{svc: svc}
}

type createPriceRuleRequest struct {
	StartDate  string `json:"startDate" binding:"required"`
	EndDate    string `json:"endDate" binding:"required"`
	PriceCents int64  `json:"priceCents" binding:"required"`
	Label      string `json:"label"`
}

// Create adds a price rule to a listing the host owns.
func (h *PriceRuleHandler) Create(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req createPriceRuleRequest
	if !bindJSON(c, &req) {
		return
	}
	start, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "startDate must be YYYY-MM-DD")
		return
	}
	end, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "endDate must be YYYY-MM-DD")
		return
	}
	rule, err := h.svc.Create(c.Request.Context(), priceruleapp.CreateInput{
		HostID:     hostID,
		PropertyID: propertyID,
		StartDate:  start,
		EndDate:    end,
		PriceCents: req.PriceCents,
		Label:      req.Label,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromPriceRule(rule))
}

// ListForProperty returns every price rule on a listing the host owns.
func (h *PriceRuleHandler) ListForProperty(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	rules, err := h.svc.ListForHost(c.Request.Context(), hostID, propertyID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.PriceRuleView, 0, len(rules))
	for _, r := range rules {
		items = append(items, dto.FromPriceRule(r))
	}
	response.OK(c, items)
}

// Delete removes a price rule on a listing the host owns.
func (h *PriceRuleHandler) Delete(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	ruleID, ok := pathUUID(c, "ruleId")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), hostID, propertyID, ruleID); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}
