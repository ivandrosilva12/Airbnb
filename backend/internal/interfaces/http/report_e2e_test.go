package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_ListingReports covers the moderation flow: a guest reports a
// listing, an admin sees it in the queue (enriched with the listing title) and
// resolves it, after which the queue is empty. It also checks duplicate-report
// prevention and admin-only authorization.
func TestEndToEnd_ListingReports(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "rep-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "rep-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "rep-admin@test.dev")
	hostTok := host.ID.String()
	guestTok := guest.ID.String()
	adminTok := admin.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Suspicious Loft", "type": "apartment", "city": "Braga", "country": "PT",
		"latitude": 41.5, "longitude": -8.4, "priceCents": 7000, "currency": "EUR", "maxGuests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)

	// A guest reports the listing.
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/reports", guestTok, map[string]any{
		"reason": "scam", "note": "Asks for off-platform payment",
	})
	mustStatus(t, rec, http.StatusCreated, "file report")
	if status := h.decode(rec)["status"].(string); status != "open" {
		t.Fatalf("status = %q, want open", status)
	}

	// A second open report from the same guest is rejected.
	if r := h.do(http.MethodPost, "/api/v1/properties/"+propID+"/reports", guestTok, map[string]any{
		"reason": "spam",
	}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate report: status = %d, want 422", r.Code)
	}

	// An invalid reason is a 422.
	if r := h.do(http.MethodPost, "/api/v1/properties/"+propID+"/reports", guestTok, map[string]any{
		"reason": "whatever",
	}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad reason: status = %d, want 422", r.Code)
	}

	// A non-admin cannot read the moderation queue.
	if r := h.do(http.MethodGet, "/api/v1/admin/reports", guestTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("guest queue: status = %d, want 403", r.Code)
	}

	// Admin sees the open report, enriched with the listing title.
	rec = h.do(http.MethodGet, "/api/v1/admin/reports", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "admin reports")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("queue items = %d, want 1", len(items))
	}
	first := items[0].(map[string]any)
	if first["propertyTitle"].(string) != "Suspicious Loft" {
		t.Fatalf("propertyTitle = %v, want Suspicious Loft", first["propertyTitle"])
	}
	reportID := first["id"].(string)

	// Admin resolves it (and could suspend the listing via the property endpoint).
	rec = h.do(http.MethodPost, "/api/v1/admin/reports/"+reportID+"/resolve", adminTok, map[string]any{
		"resolution": "Listing suspended",
	})
	mustStatus(t, rec, http.StatusOK, "resolve report")
	if status := h.decode(rec)["status"].(string); status != "resolved" {
		t.Fatalf("status = %q, want resolved", status)
	}

	// The admin can suspend the reported listing via the existing endpoint.
	rec = h.do(http.MethodPost, "/api/v1/admin/properties/"+propID+"/suspend", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "suspend listing")

	// The queue is now empty.
	rec = h.do(http.MethodGet, "/api/v1/admin/reports", adminTok, nil)
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("queue after resolve = %d, want 0", len(items))
	}
}
