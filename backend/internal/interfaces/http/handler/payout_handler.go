package handler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"

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

// Available returns the authenticated host's withdrawable balance per currency
// (net earnings minus payouts already in flight or completed).
func (h *PayoutHandler) Available(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	balances, err := h.svc.AvailableBalances(c.Request.Context(), hostID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"available": dto.FromAvailableBalances(balances)})
}

// ListDisbursements returns the authenticated host's payouts, newest first.
func (h *PayoutHandler) ListDisbursements(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	page := pageFromQuery(c)
	res, err := h.svc.ListDisbursements(c.Request.Context(), hostID, page)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.DisbursementView, 0, len(res.Items))
	for _, d := range res.Items {
		items = append(items, dto.FromDisbursement(d))
	}
	response.OK(c, dto.PageView[dto.DisbursementView]{Items: items, Total: res.Total, Limit: page.Limit, Offset: page.Offset})
}

type requestDisbursementRequest struct {
	Currency string `json:"currency" binding:"required"`
}

// RequestDisbursement pays out the host's available balance in a currency.
func (h *PayoutHandler) RequestDisbursement(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	var req requestDisbursementRequest
	if !bindJSON(c, &req) {
		return
	}
	d, err := h.svc.RequestDisbursement(c.Request.Context(), hostID, req.Currency)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromDisbursement(d))
}

// ExportCSV streams the authenticated host's full earnings ledger as a CSV
// statement (amounts in major currency units; refunds are negative).
func (h *PayoutHandler) ExportCSV(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	rows, err := h.svc.ExportEntries(c.Request.Context(), hostID)
	if err != nil {
		response.Fail(c, err)
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="airhost-earnings.csv"`)
	c.Status(http.StatusOK)

	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"date", "type", "listing", "listing_id", "booking_id", "currency", "amount"})
	for _, r := range rows {
		amount := strconv.FormatFloat(float64(r.SignedCents)/100, 'f', 2, 64)
		_ = w.Write([]string{
			r.CreatedAt.Format("2006-01-02"),
			r.Kind,
			r.PropertyTitle,
			r.PropertyID.String(),
			r.BookingID.String(),
			r.Currency,
			amount,
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		// Headers/rows may already be partly written; just log via the framework.
		_ = c.Error(fmt.Errorf("csv export: %w", err))
	}
}
