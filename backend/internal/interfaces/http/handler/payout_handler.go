package handler

import (
	payoutapp "github.com/airhost/backend/internal/application/payout"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// PayoutHandler exposes host-earnings endpoints.
type PayoutHandler struct {
	svc *payoutapp.Service
}

// NewPayoutHandler builds a PayoutHandler.
func NewPayoutHandler(svc *payoutapp.Service) *PayoutHandler { return &PayoutHandler{svc: svc} }

// Summary returns the authenticated host's earnings balance per currency.
func (h *PayoutHandler) Summary(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	balances, err := h.svc.Summary(c.Request.Context(), hostID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromBalances(balances))
}

// ListEntries returns the authenticated host's earnings ledger, newest first.
func (h *PayoutHandler) ListEntries(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	page := pageFromQuery(c)
	res, err := h.svc.ListEntries(c.Request.Context(), hostID, page)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.PayoutEntryView, 0, len(res.Items))
	for _, e := range res.Items {
		items = append(items, dto.FromPayoutEntry(e))
	}
	response.OK(c, dto.PageView[dto.PayoutEntryView]{Items: items, Total: res.Total, Limit: page.Limit, Offset: page.Offset})
}
