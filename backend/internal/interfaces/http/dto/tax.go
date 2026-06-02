package dto

import (
	"fmt"
	"time"

	"github.com/airhost/backend/internal/domain/tax"
	"github.com/airhost/backend/internal/domain/taxremittance"
	"github.com/google/uuid"
)

// TaxRuleView is the wire shape for the admin list/create response.
// Every domain field is exposed; the admin UI needs them all to
// render a sensible editor.
type TaxRuleView struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	Country         string    `json:"country"`
	City            string    `json:"city,omitempty"`
	Currency        string    `json:"currency"`
	RatePctBips     int       `json:"ratePctBips,omitempty"`
	FlatAmountCents int64     `json:"flatAmountCents,omitempty"`
	MaxNights       int       `json:"maxNights,omitempty"`
	EffectiveFrom   time.Time `json:"effectiveFrom,omitempty"`
	EffectiveUntil  time.Time `json:"effectiveUntil,omitempty"`
}

// FromTaxRule maps a domain Rule to the wire shape.
func FromTaxRule(r *tax.Rule) TaxRuleView {
	if r == nil {
		return TaxRuleView{}
	}
	return TaxRuleView{
		ID:              r.ID,
		Name:            r.Name,
		Kind:            string(r.Kind),
		Country:         r.Country,
		City:            r.City,
		Currency:        r.Currency,
		RatePctBips:     r.RatePctBips,
		FlatAmountCents: r.FlatAmountCents,
		MaxNights:       r.MaxNights,
		EffectiveFrom:   r.EffectiveFrom,
		EffectiveUntil:  r.EffectiveUntil,
	}
}

// TaxQuoteLineView mirrors one tax line in the public quote response.
type TaxQuoteLineView struct {
	RuleID      uuid.UUID `json:"ruleId"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	AmountCents int64     `json:"amountCents"`
}

// TaxQuoteView is the public preview shape. Currency lives on the
// envelope so a client doesn't have to inspect every line.
type TaxQuoteView struct {
	Lines      []TaxQuoteLineView `json:"lines"`
	TotalCents int64              `json:"totalCents"`
	Currency   string             `json:"currency"`
}

// FromTaxQuote flattens the domain Quote into wire-friendly lines.
func FromTaxQuote(q tax.Quote) TaxQuoteView {
	lines := make([]TaxQuoteLineView, 0, len(q.Lines))
	for _, l := range q.Lines {
		lines = append(lines, TaxQuoteLineView{
			RuleID: l.RuleID, Name: l.Name, Kind: string(l.Kind), AmountCents: l.AmountCents,
		})
	}
	return TaxQuoteView{Lines: lines, TotalCents: q.TotalCents, Currency: q.Currency}
}

// TaxRemittanceLineView is one tax-rule row inside a remittance report (S62).
type TaxRemittanceLineView struct {
	Name         string `json:"name"`
	AmountCents  int64  `json:"amountCents"`
	BookingCount int    `json:"bookingCount"`
}

// TaxRemittanceReportView is one (period, jurisdiction, currency) bucket
// in a remittance run. The wire shape is intentionally regulator-friendly:
// period as YYYY-MM, country/city explicit, lines pre-sorted, total at the
// top level.
type TaxRemittanceReportView struct {
	Period       string                  `json:"period"` // YYYY-MM
	Country      string                  `json:"country"`
	City         string                  `json:"city"`
	Currency     string                  `json:"currency"`
	Lines        []TaxRemittanceLineView `json:"lines"`
	TotalCents   int64                   `json:"totalCents"`
	BookingCount int                     `json:"bookingCount"`
}

// FromTaxRemittanceReport maps a domain Report to the wire shape.
func FromTaxRemittanceReport(r taxremittance.Report) TaxRemittanceReportView {
	lines := make([]TaxRemittanceLineView, 0, len(r.Lines))
	for _, l := range r.Lines {
		lines = append(lines, TaxRemittanceLineView{
			Name: l.Name, AmountCents: l.AmountCents, BookingCount: l.BookingCount,
		})
	}
	return TaxRemittanceReportView{
		Period:       fmt.Sprintf("%04d-%02d", r.Period.Year, int(r.Period.Month)),
		Country:      r.Jurisdiction.Country,
		City:         r.Jurisdiction.City,
		Currency:     r.Currency,
		Lines:        lines,
		TotalCents:   r.TotalCents,
		BookingCount: r.BookingCount,
	}
}
