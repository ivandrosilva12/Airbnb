package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_Tax_PublicQuoteSumsApplicableRules walks the realistic
// scenario for S48: an admin seeds three rules (national VAT, Lisbon
// tourist tax, fixed convention fee), then the public quote endpoint
// for a Lisbon property returns all three lines and the right total.
// Anonymous (no bearer) — proves the public route is reachable
// without auth so a UI can render the breakdown pre-sign-in.
func TestEndToEnd_Tax_PublicQuoteSumsApplicableRules(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "tax-host@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "tax-admin@test.dev")
	hostTok, adminTok := host.ID.String(), admin.ID.String()

	// Listing in Lisbon, EUR — seedInstantBookListing seeds with
	// {city: "Porto", currency: "EUR"} so we use the regular property
	// flow below to keep the city explicit.
	propID := seedTaxableLisbonListing(t, h, hostTok)

	// Admin seeds three rules.
	seedRule(t, h, adminTok, map[string]any{
		"name": "Z VAT", "kind": "percent", "country": "PT", "currency": "EUR", "ratePctBips": 2300,
	})
	seedRule(t, h, adminTok, map[string]any{
		"name": "M Tourist tax", "kind": "per_night_per_guest", "country": "PT", "city": "Lisbon",
		"currency": "EUR", "flatAmountCents": 200, "maxNights": 7,
	})
	seedRule(t, h, adminTok, map[string]any{
		"name": "A Convention fee", "kind": "per_stay", "country": "PT", "city": "Lisbon",
		"currency": "EUR", "flatAmountCents": 500,
	})

	// Public quote: 3 nights, 2 guests, subtotal 300 EUR (30000 cents).
	rec := h.do(http.MethodGet,
		"/api/v1/properties/"+propID+"/tax-quote?checkIn=2026-07-01&nights=3&guests=2&subtotalCents=30000",
		"", nil)
	mustStatus(t, rec, http.StatusOK, "public quote")
	body := h.decode(rec)

	// Expected: VAT 6900 + Tourist 1200 + Convention 500 = 8600.
	if total := int64(body["totalCents"].(float64)); total != 8600 {
		t.Fatalf("totalCents = %d, want 8600 (body: %v)", total, body)
	}
	lines := body["lines"].([]any)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if body["currency"] != "EUR" {
		t.Errorf("currency = %v, want EUR", body["currency"])
	}
	// Lines are name-sorted per the calculator contract.
	if lines[0].(map[string]any)["name"] != "A Convention fee" {
		t.Errorf("lines[0].name = %v, want A Convention fee", lines[0].(map[string]any)["name"])
	}
	if lines[2].(map[string]any)["name"] != "Z VAT" {
		t.Errorf("lines[2].name = %v, want Z VAT", lines[2].(map[string]any)["name"])
	}
}

// TestEndToEnd_Tax_QuoteSkipsRulesForOtherCity proves the
// jurisdiction filter: a Porto listing receives the national VAT
// but NOT the Lisbon-only tourist tax or convention fee. The total
// reflects only the rule that applies.
func TestEndToEnd_Tax_QuoteSkipsRulesForOtherCity(t *testing.T) {
	h := newHarness(t)
	admin := h.seedUser(domainuser.RoleAdmin, "tax-admin-porto@test.dev")
	host := h.seedUser(domainuser.RoleHost, "tax-host-porto@test.dev")
	adminTok, hostTok := admin.ID.String(), host.ID.String()

	// Use the existing Porto-seeding helper from split_payment tests.
	propID, _ := seedInstantBookListing(t, h, hostTok)

	seedRule(t, h, adminTok, map[string]any{
		"name": "VAT", "kind": "percent", "country": "PT", "currency": "EUR", "ratePctBips": 2300,
	})
	seedRule(t, h, adminTok, map[string]any{
		"name": "Lisbon tourist tax", "kind": "per_night_per_guest", "country": "PT", "city": "Lisbon",
		"currency": "EUR", "flatAmountCents": 200, "maxNights": 7,
	})

	rec := h.do(http.MethodGet,
		"/api/v1/properties/"+propID+"/tax-quote?checkIn=2026-07-01&nights=3&guests=2&subtotalCents=30000",
		"", nil)
	mustStatus(t, rec, http.StatusOK, "Porto quote")
	body := h.decode(rec)
	if total := int64(body["totalCents"].(float64)); total != 6900 {
		t.Fatalf("totalCents = %d, want 6900 (only VAT applies to Porto)", total)
	}
	lines := body["lines"].([]any)
	if len(lines) != 1 || lines[0].(map[string]any)["name"] != "VAT" {
		t.Errorf("expected only VAT line, got %v", lines)
	}
}

// TestEndToEnd_Tax_AdminCRUD_LifecycleRoundTrips covers list →
// create → delete from the admin endpoints, plus the after-delete
// quote drop. Together that's the contract a tax-rule admin UI
// would exercise.
func TestEndToEnd_Tax_AdminCRUD_LifecycleRoundTrips(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "tax-crud-host@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "tax-crud-admin@test.dev")
	hostTok, adminTok := host.ID.String(), admin.ID.String()

	propID := seedTaxableLisbonListing(t, h, hostTok)

	// Empty list to start.
	rec := h.do(http.MethodGet, "/api/v1/admin/tax-rules", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "empty list")
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("initial items = %d, want 0", len(items))
	}

	// Create a per-stay rule.
	ruleID := seedRule(t, h, adminTok, map[string]any{
		"name": "Convention fee", "kind": "per_stay", "country": "PT", "city": "Lisbon",
		"currency": "EUR", "flatAmountCents": 750,
	})

	// List shows the rule.
	rec = h.do(http.MethodGet, "/api/v1/admin/tax-rules", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "list after create")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("after create, items = %d, want 1", len(items))
	}

	// Quote picks it up.
	rec = h.do(http.MethodGet,
		"/api/v1/properties/"+propID+"/tax-quote?checkIn=2026-07-01&nights=2&guests=1&subtotalCents=20000",
		"", nil)
	mustStatus(t, rec, http.StatusOK, "quote with rule")
	if total := int64(h.decode(rec)["totalCents"].(float64)); total != 750 {
		t.Fatalf("quote with single per-stay rule = %d, want 750", total)
	}

	// Delete it and the quote falls back to zero.
	rec = h.do(http.MethodDelete, "/api/v1/admin/tax-rules/"+ruleID, adminTok, nil)
	mustStatus(t, rec, http.StatusNoContent, "delete rule")

	rec = h.do(http.MethodGet,
		"/api/v1/properties/"+propID+"/tax-quote?checkIn=2026-07-01&nights=2&guests=1&subtotalCents=20000",
		"", nil)
	mustStatus(t, rec, http.StatusOK, "quote after delete")
	if total := int64(h.decode(rec)["totalCents"].(float64)); total != 0 {
		t.Errorf("after delete, totalCents = %d, want 0", total)
	}
}

// seedTaxableLisbonListing creates a Lisbon EUR listing the quote
// tests target. seedInstantBookListing is Porto-by-design, so this
// helper makes the city explicit for tax scenarios.
func seedTaxableLisbonListing(t *testing.T, h *harness, hostTok string) string {
	t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Lisbon flat", "type": "apartment", "city": "Lisbon", "country": "PT",
		"latitude": 38.7, "longitude": -9.1, "priceCents": 10000, "currency": "EUR", "maxGuests": 4,
	})
	mustStatus(t, rec, http.StatusCreated, "seed Lisbon listing")
	id := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, id)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+id+"/publish", hostTok, nil), http.StatusOK, "publish Lisbon listing")
	return id
}

// seedRule POSTs a tax rule and returns its id. Wraps the admin
// create endpoint so the assertion-heavy test bodies stay readable.
func seedRule(t *testing.T, h *harness, adminTok string, body map[string]any) string {
	t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/admin/tax-rules", adminTok, body)
	mustStatus(t, rec, http.StatusCreated, "create rule "+body["name"].(string))
	return h.decode(rec)["id"].(string)
}
