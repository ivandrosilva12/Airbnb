package handler

import (
	"net/http"
	"strconv"

	taxremittanceapp "github.com/airhost/backend/internal/application/taxremittance"
	"github.com/airhost/backend/internal/domain/taxremittance"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// TaxRemittanceHandler exposes the read-only remittance read-model (S62).
// Admin-only — the report is operator-facing and contains aggregate-level
// totals not meant for hosts or guests.
type TaxRemittanceHandler struct {
	svc *taxremittanceapp.Service
}

// NewTaxRemittanceHandler builds a TaxRemittanceHandler.
func NewTaxRemittanceHandler(svc *taxremittanceapp.Service) *TaxRemittanceHandler {
	return &TaxRemittanceHandler{svc: svc}
}

// AdminPeriod returns the per-jurisdiction remittance breakdown for a
// calendar month.
//
//	GET /api/v1/admin/tax/remittance?year=2026&month=5
//
// 400 when year/month are missing or out of range. Always returns a list
// (possibly empty) — an empty period produces [], not 404, so the operator
// can tell "we ran the report and nothing was owed" apart from "I forgot
// to call the endpoint".
func (h *TaxRemittanceHandler) AdminPeriod(c *gin.Context) {
	year, err := strconv.Atoi(c.Query("year"))
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "year must be an integer")
		return
	}
	month, err := strconv.Atoi(c.Query("month"))
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "month must be an integer")
		return
	}
	period, err := taxremittance.NewPeriod(year, month)
	if err != nil {
		response.Fail(c, err)
		return
	}
	reports, err := h.svc.Generate(c.Request.Context(), period)
	if err != nil {
		response.Fail(c, err)
		return
	}
	views := make([]dto.TaxRemittanceReportView, 0, len(reports))
	for _, r := range reports {
		views = append(views, dto.FromTaxRemittanceReport(r))
	}
	response.OK(c, gin.H{"items": views})
}
