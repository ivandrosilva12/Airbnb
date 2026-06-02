package http_test

import (
	"net/http"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_TaxBooking_JurisdictionLinesLandOnBookingResponse is
// the cornerstone test for S49: an admin seeds rules, a guest books
// a Lisbon listing, and the 201 booking response carries
// jurisdictionTaxLines + jurisdictionTax + a total that reflects the
// added tax. If a future refactor breaks the TaxQuoter port, the
// pricing struct's new fields, or the DTO surface, this test fails.
func TestEndToEnd_TaxBooking_JurisdictionLinesLandOnBookingResponse(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "tax-book-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "tax-book-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "tax-book-admin@test.dev")
	hostTok, guestTok, adminTok := host.ID.String(), guest.ID.String(), admin.ID.String()

	// Lisbon listing, EUR — seedTaxableLisbonListing lives in
	// tax_e2e_test.go and sets priceCents=10000, no listing tax rate.
	propID := seedTaxableLisbonListing(t, h, hostTok)

	// Admin seeds two rules: national VAT 23%, fixed convention fee.
	// Tourist tax is deliberately omitted so the per-night-per-guest
	// math doesn't dominate and we can read the totals at a glance.
	seedRule(t, h, adminTok, map[string]any{
		"name": "VAT", "kind": "percent", "country": "PT", "currency": "EUR", "ratePctBips": 2300,
	})
	seedRule(t, h, adminTok, map[string]any{
		"name": "Convention fee", "kind": "per_stay", "country": "PT", "city": "Lisbon",
		"currency": "EUR", "flatAmountCents": 500,
	})

	// Book 2 nights @ 100 EUR = 20000 cents subtotal. CleaningFee = 0
	// on the seeded listing, so taxable base = 20000.
	in := time.Now().UTC().AddDate(0, 0, 60).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 62).Format("2006-01-02")
	rec := h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "create booking with tax rules active")
	body := h.decode(rec)

	// Sanity-check the wire shape — both fields must be present, even
	// when the host has rules wired (no omitempty trap).
	lines, ok := body["jurisdictionTaxLines"].([]any)
	if !ok || len(lines) != 2 {
		t.Fatalf("jurisdictionTaxLines = %v, want 2 lines (body: %v)", body["jurisdictionTaxLines"], body)
	}
	// Lines arrive in the calculator's name-sorted order: Convention < VAT.
	if lines[0].(map[string]any)["name"] != "Convention fee" {
		t.Errorf("lines[0].name = %v, want Convention fee", lines[0].(map[string]any)["name"])
	}
	if lines[1].(map[string]any)["name"] != "VAT" {
		t.Errorf("lines[1].name = %v, want VAT", lines[1].(map[string]any)["name"])
	}

	jurTax, _ := body["jurisdictionTax"].(map[string]any)
	jurCents := int64(jurTax["amountCents"].(float64))
	// VAT 23% of 20000 = 4600; convention 500. Sum = 5100.
	if jurCents != 5100 {
		t.Fatalf("jurisdictionTax.amountCents = %d, want 5100", jurCents)
	}

	// Total must include those 5100 cents on top of the existing
	// breakdown (subtotal + service fee, since cleaning + extras + legacy
	// tax + deposit are all 0 on the seeded listing).
	//   subtotal 20000 + service 10% * 20000 = 2000 + jurTax 5100 = 27100
	total := int64(body["totalPrice"].(map[string]any)["amountCents"].(float64))
	if total != 27100 {
		t.Fatalf("totalPrice = %d, want 27100 (20000 + 2000 service + 5100 jurisdiction tax)", total)
	}
}

// TestEndToEnd_TaxBooking_NoRules_NoLines confirms the additive
// nature: a listing with no matching rules produces an empty
// jurisdictionTaxLines and a zero jurisdictionTax — the legacy
// pricing path is unchanged.
func TestEndToEnd_TaxBooking_NoRules_NoLines(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "no-tax-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "no-tax-guest@test.dev")
	hostTok, guestTok := host.ID.String(), guest.ID.String()

	// Use the Porto seed (no rules ever created in this test).
	propID, _ := seedInstantBookListing(t, h, hostTok)

	in := time.Now().UTC().AddDate(0, 0, 90).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 92).Format("2006-01-02")
	rec := h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "create booking without rules")
	body := h.decode(rec)
	if lines, ok := body["jurisdictionTaxLines"]; ok && lines != nil {
		// omitempty means absent OR empty — both fine; if present must be empty.
		if list, _ := lines.([]any); len(list) != 0 {
			t.Errorf("expected no jurisdictionTaxLines, got %v", list)
		}
	}
	jurTax, _ := body["jurisdictionTax"].(map[string]any)
	if cents := int64(jurTax["amountCents"].(float64)); cents != 0 {
		t.Errorf("jurisdictionTax.amountCents = %d, want 0", cents)
	}
}
