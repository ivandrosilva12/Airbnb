package handler

import (
	"net/http"
	"strconv"
	"time"

	auditapp "github.com/airhost/backend/internal/application/audit"
	taxapp "github.com/airhost/backend/internal/application/tax"
	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/tax"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// TaxHandler exposes the tax BC's HTTP surface: the public quote on a
// property and the admin CRUD-lite over the rule table.
// audit is optional (S54) — wired at the composition root via
// WithAudit so AdminCreate / AdminDelete record a trail. Tax-rule
// edits are uniquely high-stakes (they change every receipt going
// forward) — the audit row is the regulator-ready answer to
// "who turned VAT on/off on 2026-07-01?".
type TaxHandler struct {
	svc   *taxapp.Service
	audit *auditapp.Service
}

// NewTaxHandler wires a TaxHandler.
func NewTaxHandler(svc *taxapp.Service) *TaxHandler {
	return &TaxHandler{svc: svc}
}

// WithAudit attaches the audit service so the admin CRUD records
// a trail (S54). Returns the receiver for chaining.
func (h *TaxHandler) WithAudit(a *auditapp.Service) *TaxHandler {
	h.audit = a
	return h
}

// Quote serves the public tax preview for a stay. The endpoint is
// unauthenticated (no bearer required) so a UI can render the
// breakdown to anonymous browsers before sign-in. The caller passes
// the stay shape in query params; the property is resolved by id
// and its (country, city, currency) drive the rule lookup.
//
// Example:
//
//	GET /api/v1/properties/<uuid>/tax-quote?checkIn=2026-07-01&nights=3&guests=2&subtotalCents=30000
func (h *TaxHandler) Quote(c *gin.Context) {
	propID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	checkIn, err := time.Parse("2006-01-02", c.Query("checkIn"))
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "checkIn must be YYYY-MM-DD")
		return
	}
	nights, _ := strconv.Atoi(c.Query("nights"))
	guests, _ := strconv.Atoi(c.Query("guests"))
	subtotal, _ := strconv.ParseInt(c.Query("subtotalCents"), 10, 64)

	q, err := h.svc.QuoteForProperty(c.Request.Context(), taxapp.QuoteInput{
		PropertyID:    propID,
		CheckIn:       checkIn,
		Nights:        nights,
		Guests:        guests,
		SubtotalCents: subtotal,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromTaxQuote(q))
}

// AdminList serves the rule table for the admin console.
func (h *TaxHandler) AdminList(c *gin.Context) {
	if _, ok := requireUser(c); !ok {
		return
	}
	rules, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]dto.TaxRuleView, 0, len(rules))
	for _, r := range rules {
		out = append(out, dto.FromTaxRule(r))
	}
	response.OK(c, gin.H{"items": out})
}

type createTaxRuleRequest struct {
	Name            string    `json:"name" binding:"required"`
	Kind            string    `json:"kind" binding:"required"`
	Country         string    `json:"country"`
	City            string    `json:"city"`
	Currency        string    `json:"currency" binding:"required"`
	RatePctBips     int       `json:"ratePctBips"`
	FlatAmountCents int64     `json:"flatAmountCents"`
	MaxNights       int       `json:"maxNights"`
	EffectiveFrom   time.Time `json:"effectiveFrom"`
	EffectiveUntil  time.Time `json:"effectiveUntil"`
}

// AdminCreate inserts a new rule. The handler bind-only validates
// the required wrapper; the per-kind knob check fires inside the
// domain constructor so the same guard applies to every caller (CLI
// seeders included). Records an audit row on success (S54).
func (h *TaxHandler) AdminCreate(c *gin.Context) {
	adminID, ok := requireUser(c)
	if !ok {
		return
	}
	var req createTaxRuleRequest
	if !bindJSON(c, &req) {
		return
	}
	r, err := h.svc.Create(c.Request.Context(), taxapp.CreateInput{
		Name:            req.Name,
		Kind:            tax.Kind(req.Kind),
		Country:         req.Country,
		City:            req.City,
		Currency:        req.Currency,
		RatePctBips:     req.RatePctBips,
		FlatAmountCents: req.FlatAmountCents,
		MaxNights:       req.MaxNights,
		EffectiveFrom:   req.EffectiveFrom,
		EffectiveUntil:  req.EffectiveUntil,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	if h.audit != nil {
		if err := h.audit.Record(c.Request.Context(), auditapp.RecordInput{
			ActorID: adminID, Action: audit.ActionTaxRuleCreate,
			TargetType: audit.TargetTaxRule, TargetID: r.ID,
			Metadata: map[string]any{
				"name":     r.Name,
				"kind":     string(r.Kind),
				"country":  r.Country,
				"city":     r.City,
				"currency": r.Currency,
			},
		}); err != nil {
			response.Fail(c, err)
			return
		}
	}
	response.Created(c, dto.FromTaxRule(r))
}

// AdminDelete removes a rule by id. Idempotent (the repo treats a
// missing row as a successful delete) — we still return 204 on the
// happy path so the admin UI can refresh the list. Records an audit
// row on success (S54).
func (h *TaxHandler) AdminDelete(c *gin.Context) {
	adminID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	if h.audit != nil {
		if err := h.audit.Record(c.Request.Context(), auditapp.RecordInput{
			ActorID: adminID, Action: audit.ActionTaxRuleDelete,
			TargetType: audit.TargetTaxRule, TargetID: id,
		}); err != nil {
			response.Fail(c, err)
			return
		}
	}
	response.NoContent(c)
}

