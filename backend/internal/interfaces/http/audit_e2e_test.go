package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_AuditTrail_LandsOnAdminDisputeDecision walks a real
// admin action end-to-end and asserts the audit endpoint then surfaces
// the row: guest opens dispute, admin rejects, admin reads /admin/audit
// and finds the dispute.reject entry with the resolution text in
// metadata.
//
// This is the canonical proof that S45 actually wired through — the
// service exists, the handler decorates with WithAudit, the audit
// service writes, and the read endpoint serves what was written. If a
// future refactor breaks any of those links, this test fails loudly.
func TestEndToEnd_AuditTrail_LandsOnAdminDisputeDecision(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "audit-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "audit-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "audit-admin@test.dev")
	guestTok, adminTok := guest.ID.String(), admin.ID.String()

	// Seed a completed stay the guest can open a dispute against.
	b := h.seedCompletedStay(guest.ID, host.ID, "Audit Flat")

	rec := h.do(http.MethodPost, "/api/v1/bookings/"+b.ID.String()+"/disputes", guestTok, map[string]any{
		"kind": "refund", "reason": "noisy lobby",
		"requestedAmountCents": 5000, "currency": "EUR",
	})
	mustStatus(t, rec, http.StatusCreated, "open dispute")
	disputeID := h.decode(rec)["id"].(string)

	// Admin rejects with a short resolution string.
	rec = h.do(http.MethodPost, "/api/v1/admin/disputes/"+disputeID+"/reject", adminTok, map[string]any{
		"resolution": "no evidence",
	})
	mustStatus(t, rec, http.StatusOK, "admin reject")

	// Now read the audit trail. We expect one row with the right
	// action + target + actor + metadata.resolution.
	rec = h.do(http.MethodGet, "/api/v1/admin/audit", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "list audit")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("audit items = %d, want 1 (body: %s)", len(items), rec.Body.String())
	}
	row := items[0].(map[string]any)
	if row["action"] != "dispute.reject" {
		t.Fatalf("action = %v, want dispute.reject", row["action"])
	}
	if row["targetType"] != "dispute" {
		t.Fatalf("targetType = %v, want dispute", row["targetType"])
	}
	if row["targetId"] != disputeID {
		t.Fatalf("targetId = %v, want %s", row["targetId"], disputeID)
	}
	if row["actorId"] != admin.ID.String() {
		t.Fatalf("actorId = %v, want %s", row["actorId"], admin.ID.String())
	}
	meta, _ := row["metadata"].(map[string]any)
	if meta["resolution"] != "no evidence" {
		t.Fatalf("metadata.resolution = %v, want 'no evidence'", meta["resolution"])
	}
}

// TestEndToEnd_AuditTrail_FiltersByTarget confirms the targetId query
// param narrows the result set — proves the filter passes through
// the handler → service → repo and back into the wire shape.
func TestEndToEnd_AuditTrail_FiltersByTarget(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "audit-host2@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "audit-admin2@test.dev")
	hostTok, adminTok := host.ID.String(), admin.ID.String()

	// Seed two listings and suspend both — generating 2 audit rows.
	// Inline create+publish (no shared seed helper for non-instant-book).
	seedListing := func(title string) string {
		rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
			"title": title, "type": "apartment", "city": "Lisbon", "country": "PT",
			"latitude": 38.7, "longitude": -9.1, "priceCents": 10000, "currency": "EUR", "maxGuests": 2,
		})
		mustStatus(t, rec, http.StatusCreated, "seed "+title)
		id := h.decode(rec)["id"].(string)
		uploadPhoto(t, h, hostTok, id)
		mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+id+"/publish", hostTok, nil), http.StatusOK, "publish "+title)
		return id
	}
	p1ID := seedListing("First Flat")
	p2ID := seedListing("Second Flat")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/admin/properties/"+p1ID+"/suspend", adminTok, nil), http.StatusOK, "suspend p1")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/admin/properties/"+p2ID+"/suspend", adminTok, nil), http.StatusOK, "suspend p2")

	// Unfiltered: both rows.
	rec := h.do(http.MethodGet, "/api/v1/admin/audit", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "list audit")
	if got := len(h.decode(rec)["items"].([]any)); got != 2 {
		t.Fatalf("unfiltered len = %d, want 2", got)
	}

	// Filtered by targetId: only the matching row.
	rec = h.do(http.MethodGet, "/api/v1/admin/audit?targetId="+p1ID, adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "filtered list")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("filtered len = %d, want 1", len(items))
	}
	if items[0].(map[string]any)["targetId"] != p1ID {
		t.Fatalf("filtered targetId mismatch: %v", items[0])
	}
}
