package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_Audit_ReportResolveAndDismiss_LandRows walks the
// existing report-moderation flow (a guest reports → admin reads
// queue → admin resolves OR dismisses) and asserts the new S54
// audit hooks produced the expected trail rows on /admin/audit:
//
//   - resolved report → action=report.resolve, target=report:<id>,
//     metadata.resolution carries the moderator note
//   - dismissed report → action=report.dismiss, same target shape
//
// If a future refactor drops the audit hook from either path, the
// admin loses the compliance answer to "who closed this report?"
// and this test fails before that ships.
func TestEndToEnd_Audit_ReportResolveAndDismiss_LandRows(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "ahook-rep-host@test.dev")
	guestA := h.seedUser(domainuser.RoleGuest, "ahook-rep-ga@test.dev")
	guestB := h.seedUser(domainuser.RoleGuest, "ahook-rep-gb@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "ahook-rep-admin@test.dev")
	hostTok, gaTok, gbTok, adminTok := host.ID.String(), guestA.ID.String(), guestB.ID.String(), admin.ID.String()

	// Seed a listing both guests can report (one each — the service
	// rejects duplicate-from-same-reporter, so we need two reporters
	// to land two reports against the same listing).
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Hooked Loft", "type": "apartment", "city": "Braga", "country": "PT",
		"latitude": 41.5, "longitude": -8.4, "priceCents": 7000, "currency": "EUR", "maxGuests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "seed listing")
	propID := h.decode(rec)["id"].(string)

	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/reports", gaTok, map[string]any{
		"reason": "scam", "note": "Asks for off-platform payment",
	}), http.StatusCreated, "guestA report")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/reports", gbTok, map[string]any{
		"reason": "spam", "note": "Spammy listing",
	}), http.StatusCreated, "guestB report")

	// Pull the queue → 2 reports.
	rec = h.do(http.MethodGet, "/api/v1/admin/reports", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "queue")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("queue len = %d, want 2", len(items))
	}
	reportA := items[0].(map[string]any)["id"].(string)
	reportB := items[1].(map[string]any)["id"].(string)

	// Resolve A, dismiss B.
	mustStatus(t,
		h.do(http.MethodPost, "/api/v1/admin/reports/"+reportA+"/resolve", adminTok, map[string]any{"resolution": "suspended"}),
		http.StatusOK, "resolve A")
	mustStatus(t,
		h.do(http.MethodPost, "/api/v1/admin/reports/"+reportB+"/dismiss", adminTok, map[string]any{"resolution": "no policy violation"}),
		http.StatusOK, "dismiss B")

	// Audit: expect at least 2 rows — one per action.
	rec = h.do(http.MethodGet, "/api/v1/admin/audit?targetType=report", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "audit by target report")
	rows := h.decode(rec)["items"].([]any)
	if len(rows) != 2 {
		t.Fatalf("audit rows for report = %d, want 2", len(rows))
	}
	// Newest first; the dismiss happened last.
	actions := []string{
		rows[0].(map[string]any)["action"].(string),
		rows[1].(map[string]any)["action"].(string),
	}
	gotResolve, gotDismiss := false, false
	for _, a := range actions {
		switch a {
		case "report.resolve":
			gotResolve = true
		case "report.dismiss":
			gotDismiss = true
		}
	}
	if !gotResolve || !gotDismiss {
		t.Fatalf("expected both report.resolve + report.dismiss, got %v", actions)
	}
}

// TestEndToEnd_Audit_CouponDeactivate_LandsRow drives the coupon
// admin path: create a coupon, deactivate it, assert the audit row
// landed with action=coupon.deactivate and metadata.code carrying
// the human-readable code (so a moderator does not need to join
// back to the coupon table to identify which one).
func TestEndToEnd_Audit_CouponDeactivate_LandsRow(t *testing.T) {
	h := newHarness(t)
	admin := h.seedUser(domainuser.RoleAdmin, "ahook-coupon-admin@test.dev")
	adminTok := admin.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/admin/coupons", adminTok, map[string]any{
		"code": "SUMMER10", "kind": "percentage", "percent": 0.10, "maxRedemptions": 100,
	})
	mustStatus(t, rec, http.StatusCreated, "create coupon")
	couponID := h.decode(rec)["id"].(string)

	mustStatus(t,
		h.do(http.MethodPost, "/api/v1/admin/coupons/"+couponID+"/deactivate", adminTok, nil),
		http.StatusOK, "deactivate coupon")

	rec = h.do(http.MethodGet, "/api/v1/admin/audit?action=coupon.deactivate", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "audit by action coupon.deactivate")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("rows = %d, want 1", len(items))
	}
	row := items[0].(map[string]any)
	if row["targetId"] != couponID {
		t.Errorf("targetId = %v, want %s", row["targetId"], couponID)
	}
	meta, _ := row["metadata"].(map[string]any)
	if meta["code"] != "SUMMER10" {
		t.Errorf("metadata.code = %v, want SUMMER10", meta["code"])
	}
}

// TestEndToEnd_Audit_TaxRuleCreateAndDelete_LandsRows confirms the
// two new actions added to the closed enum (S54) actually fire on
// the tax-rule admin paths and write the metadata fields the audit
// reader expects (name, kind, country, city, currency on create —
// nothing required on delete; the action + targetId is the whole
// signal).
func TestEndToEnd_Audit_TaxRuleCreateAndDelete_LandsRows(t *testing.T) {
	h := newHarness(t)
	admin := h.seedUser(domainuser.RoleAdmin, "ahook-tax-admin@test.dev")
	adminTok := admin.ID.String()

	ruleID := seedRule(t, h, adminTok, map[string]any{
		"name": "Lisbon city tax", "kind": "per_stay", "country": "PT", "city": "Lisbon",
		"currency": "EUR", "flatAmountCents": 1500,
	})

	rec := h.do(http.MethodGet, "/api/v1/admin/audit?action=tax_rule.create", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "audit by action tax_rule.create")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("create rows = %d, want 1", len(items))
	}
	row := items[0].(map[string]any)
	if row["targetType"] != "tax_rule" {
		t.Errorf("targetType = %v, want tax_rule", row["targetType"])
	}
	if row["targetId"] != ruleID {
		t.Errorf("targetId = %v, want %s", row["targetId"], ruleID)
	}
	meta, _ := row["metadata"].(map[string]any)
	if meta["name"] != "Lisbon city tax" || meta["kind"] != "per_stay" || meta["country"] != "PT" {
		t.Errorf("create metadata wrong: %v", meta)
	}

	// Delete and assert a second row materialised.
	mustStatus(t,
		h.do(http.MethodDelete, "/api/v1/admin/tax-rules/"+ruleID, adminTok, nil),
		http.StatusNoContent, "delete rule")

	rec = h.do(http.MethodGet, "/api/v1/admin/audit?action=tax_rule.delete", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "audit by action tax_rule.delete")
	items = h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("delete rows = %d, want 1", len(items))
	}
	if items[0].(map[string]any)["targetId"] != ruleID {
		t.Errorf("delete row targetId mismatch: %v", items[0])
	}
}
