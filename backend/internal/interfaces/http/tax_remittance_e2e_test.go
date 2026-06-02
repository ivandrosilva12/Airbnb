package http_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// seedSettledBookingForRemittance stores a property in the given jurisdiction
// and a confirmed booking whose check-out lands inside the target month, with
// JurisdictionTaxLines pre-populated. Bypasses the booking API so the test
// can choose past dates and inject the per-rule tax breakdown directly.
func (h *harness) seedSettledBookingForRemittance(
	hostID, guestID uuid.UUID, city string, checkOut time.Time, lines []booking.TaxLine,
) *booking.Booking {
	h.t.Helper()
	ctx := context.Background()
	price, _ := shared.NewMoney(10000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: city, Country: "PT", Latitude: 41.1, Longitude: -8.6}
	p, err := property.NewProperty(hostID, "Remittance "+city, "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		h.t.Fatalf("new property: %v", err)
	}
	if err := h.propertyRepo.Create(ctx, p); err != nil {
		h.t.Fatalf("store property: %v", err)
	}
	// Two-night stay ending on checkOut. Bypass NewDateRange because it
	// rejects past dates and we deliberately want a historical period so
	// the report can be queried by (year, month).
	truncate := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	dr := booking.DateRange{CheckIn: truncate(checkOut.AddDate(0, 0, -2)), CheckOut: truncate(checkOut)}
	// Use a permissive future-dated range to satisfy NewBooking's validators,
	// then overwrite Dates with the historical range we actually want.
	tempStart := time.Now().UTC().AddDate(0, 0, 30)
	tempDR, err := booking.NewDateRange(tempStart, tempStart.AddDate(0, 0, 2))
	if err != nil {
		h.t.Fatalf("temp date range: %v", err)
	}
	b, err := booking.NewBooking(p.ID, guestID, tempDR, 1, price, cleaning, 0, booking.Discounts{})
	if err != nil {
		h.t.Fatalf("new booking: %v", err)
	}
	b.Dates = dr
	_ = b.Confirm()
	// Inject the per-jurisdiction tax breakdown the remittance service will
	// aggregate. Sum the line cents into JurisdictionTax so the field is
	// consistent with the lines, mirroring what ComputePricing would do.
	var jurTotal int64
	for _, l := range lines {
		jurTotal += l.AmountCents
	}
	b.Pricing.JurisdictionTaxLines = append([]booking.TaxLine(nil), lines...)
	b.Pricing.JurisdictionTax, _ = shared.NewMoney(jurTotal, "EUR")
	if err := h.bookingRepo.Create(ctx, b); err != nil {
		h.t.Fatalf("store booking: %v", err)
	}
	return b
}

// TestEndToEnd_TaxRemittance_GroupsByJurisdictionAndAggregatesLines proves
// S62 end-to-end: two settled bookings in Porto + one in Lisbon land in the
// same calendar month, and the admin remittance endpoint returns one report
// per (country, city, currency) with lines summed per rule name.
func TestEndToEnd_TaxRemittance_GroupsByJurisdictionAndAggregatesLines(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "remit-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "remit-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "remit-admin@test.dev")
	adminTok := admin.ID.String()

	// All check-outs land in the same period (2025-04). Two Porto bookings
	// + one Lisbon booking. VAT applies everywhere; Porto adds city tax.
	period := time.Date(2025, 4, 15, 12, 0, 0, 0, time.UTC)
	h.seedSettledBookingForRemittance(host.ID, guest.ID, "Porto", period, []booking.TaxLine{
		{Name: "VAT (PT)", AmountCents: 2300},
		{Name: "City tax (Porto)", AmountCents: 400},
	})
	h.seedSettledBookingForRemittance(host.ID, guest.ID, "Porto", period.AddDate(0, 0, 2), []booking.TaxLine{
		{Name: "VAT (PT)", AmountCents: 1500},
		{Name: "City tax (Porto)", AmountCents: 300},
	})
	h.seedSettledBookingForRemittance(host.ID, guest.ID, "Lisbon", period.AddDate(0, 0, 5), []booking.TaxLine{
		{Name: "VAT (PT)", AmountCents: 5000},
		{Name: "City tax (Lisbon)", AmountCents: 800},
	})
	// And one booking in a different period — the report must NOT include it.
	h.seedSettledBookingForRemittance(host.ID, guest.ID, "Porto", time.Date(2025, 5, 10, 12, 0, 0, 0, time.UTC), []booking.TaxLine{
		{Name: "VAT (PT)", AmountCents: 9999},
	})

	rec := h.do(http.MethodGet, "/api/v1/admin/tax/remittance?year=2025&month=4", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "list remittance")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("reports = %d, want 2 (Porto + Lisbon, body: %s)", len(items), rec.Body.String())
	}

	// Reports are sorted by (country, city); Lisbon comes before Porto.
	lis := items[0].(map[string]any)
	if lis["city"] != "Lisbon" {
		t.Fatalf("first city = %v, want Lisbon", lis["city"])
	}
	if lis["totalCents"].(float64) != 5800 {
		t.Fatalf("Lisbon total = %v, want 5800", lis["totalCents"])
	}
	if lis["bookingCount"].(float64) != 1 {
		t.Fatalf("Lisbon bookingCount = %v, want 1", lis["bookingCount"])
	}

	prt := items[1].(map[string]any)
	if prt["city"] != "Porto" {
		t.Fatalf("second city = %v, want Porto", prt["city"])
	}
	if prt["period"] != "2025-04" {
		t.Fatalf("period = %v, want 2025-04", prt["period"])
	}
	if prt["country"] != "PT" {
		t.Fatalf("country = %v, want PT", prt["country"])
	}
	if prt["currency"] != "EUR" {
		t.Fatalf("currency = %v, want EUR", prt["currency"])
	}
	if prt["totalCents"].(float64) != 4500 { // 2300+1500 + 400+300 = 4500
		t.Fatalf("Porto total = %v, want 4500", prt["totalCents"])
	}
	if prt["bookingCount"].(float64) != 2 {
		t.Fatalf("Porto bookingCount = %v, want 2", prt["bookingCount"])
	}

	// Porto lines: 2 entries (VAT, City tax), pre-sorted by name.
	prtLines := prt["lines"].([]any)
	if len(prtLines) != 2 {
		t.Fatalf("Porto lines = %d, want 2", len(prtLines))
	}
	cityLine := prtLines[0].(map[string]any)
	if cityLine["name"] != "City tax (Porto)" {
		t.Fatalf("Porto first line = %v, want City tax (Porto)", cityLine["name"])
	}
	if cityLine["amountCents"].(float64) != 700 { // 400+300
		t.Fatalf("Porto city total = %v, want 700", cityLine["amountCents"])
	}
	if cityLine["bookingCount"].(float64) != 2 {
		t.Fatalf("Porto city booking count = %v, want 2", cityLine["bookingCount"])
	}
	vatLine := prtLines[1].(map[string]any)
	if vatLine["name"] != "VAT (PT)" {
		t.Fatalf("Porto second line = %v, want VAT (PT)", vatLine["name"])
	}
	if vatLine["amountCents"].(float64) != 3800 { // 2300+1500
		t.Fatalf("Porto VAT total = %v, want 3800", vatLine["amountCents"])
	}
}

// TestEndToEnd_TaxRemittance_EmptyPeriodReturnsEmptyList ensures the
// endpoint returns [] (200) rather than 404 when no bookings settled in
// the requested month — so the operator can distinguish "ran the report,
// nothing owed" from "forgot to call the API".
func TestEndToEnd_TaxRemittance_EmptyPeriodReturnsEmptyList(t *testing.T) {
	h := newHarness(t)
	admin := h.seedUser(domainuser.RoleAdmin, "remit-empty-admin@test.dev")
	adminTok := admin.ID.String()

	rec := h.do(http.MethodGet, "/api/v1/admin/tax/remittance?year=2030&month=7", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "list empty remittance")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("empty period reports = %d, want 0", len(items))
	}
}

// TestEndToEnd_TaxRemittance_RejectsBadInput — out-of-range or non-integer
// year/month yields 400 so a misclicked URL surfaces fast.
func TestEndToEnd_TaxRemittance_RejectsBadInput(t *testing.T) {
	h := newHarness(t)
	admin := h.seedUser(domainuser.RoleAdmin, "remit-bad-admin@test.dev")
	adminTok := admin.ID.String()

	mustStatus(t, h.do(http.MethodGet, "/api/v1/admin/tax/remittance?year=&month=4", adminTok, nil), http.StatusBadRequest, "missing year")
	mustStatus(t, h.do(http.MethodGet, "/api/v1/admin/tax/remittance?year=2025&month=13", adminTok, nil), http.StatusUnprocessableEntity, "month out of range")
	mustStatus(t, h.do(http.MethodGet, "/api/v1/admin/tax/remittance?year=1990&month=4", adminTok, nil), http.StatusUnprocessableEntity, "year out of range")
}

// TestEndToEnd_TaxRemittance_ForbidsNonAdmin keeps the operator-only report
// behind the admin gate — a guest token gets 403, not the report.
func TestEndToEnd_TaxRemittance_ForbidsNonAdmin(t *testing.T) {
	h := newHarness(t)
	guest := h.seedUser(domainuser.RoleGuest, "remit-guest-spy@test.dev")
	guestTok := guest.ID.String()

	rec := h.do(http.MethodGet, "/api/v1/admin/tax/remittance?year=2025&month=4", guestTok, nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("non-admin should not see remittance (got 200)")
	}
}
