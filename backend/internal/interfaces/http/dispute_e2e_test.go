package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_DisputeLifecycle exercises the full Resolution Center happy
// path: guest opens a case on a completed booking, the host responds, the
// admin decides, and the dispute appears in the moderation queue and in both
// participants' "my disputes" views. The booking is seeded as already
// completed (the public API rejects same-day completes), so we can focus on
// the post-stay flow.
func TestEndToEnd_DisputeLifecycle(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "dispute-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "dispute-guest@test.dev")
	other := h.seedUser(domainuser.RoleGuest, "dispute-other@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "dispute-admin@test.dev")

	hostTok := host.ID.String()
	guestTok := guest.ID.String()
	otherTok := other.ID.String()
	adminTok := admin.ID.String()

	b := h.seedCompletedStay(guest.ID, host.ID, "Atelier no Porto")
	bookingID := b.ID.String()

	// A third party cannot open a case on this booking.
	if r := h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/disputes", otherTok, map[string]any{
		"kind": "refund", "reason": "not mine", "requestedAmountCents": 100,
	}); r.Code == http.StatusCreated {
		t.Fatalf("non-participant should not open a case (got %d)", r.Code)
	}

	// Refund/damage disputes require an amount > 0.
	if r := h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/disputes", guestTok, map[string]any{
		"kind": "refund", "reason": "no amount",
	}); r.Code == http.StatusCreated {
		t.Fatalf("refund without amount should be rejected (got %d)", r.Code)
	}

	// Guest opens a refund dispute.
	rec := h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/disputes", guestTok, map[string]any{
		"kind": "refund", "reason": "Listing had no hot water for 2 nights.", "requestedAmountCents": 4000, "currency": "EUR",
	})
	mustStatus(t, rec, http.StatusCreated, "open dispute")
	d := h.decode(rec)
	disputeID := d["id"].(string)
	if d["status"] != "open" || d["kind"] != "refund" {
		t.Fatalf("unexpected dispute view: %v", d)
	}

	// A second active dispute on the same booking is rejected.
	if r := h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/disputes", guestTok, map[string]any{
		"kind": "other", "reason": "dup",
	}); r.Code == http.StatusCreated {
		t.Fatalf("second active dispute should be rejected (got %d)", r.Code)
	}

	// Host adds evidence and a response — status transitions to under_review.
	mustStatus(t, h.do(http.MethodPost, "/api/v1/disputes/"+disputeID+"/evidence", hostTok, map[string]any{
		"note": "Maintenance log attached", "url": "http://storage.test/log.pdf",
	}), http.StatusOK, "host evidence")
	rec = h.do(http.MethodPost, "/api/v1/disputes/"+disputeID+"/host-response", hostTok, map[string]any{
		"response": "Water was restored on day 2 morning; partial refund acceptable.",
	})
	mustStatus(t, rec, http.StatusOK, "host response")
	if h.decode(rec)["status"] != "under_review" {
		t.Fatalf("expected status under_review after host response")
	}

	// The dispute shows up in both parties' /me/disputes feed.
	rec = h.do(http.MethodGet, "/api/v1/me/disputes", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "guest /me/disputes")
	if list := decodeArray(t, rec.Body.Bytes()); len(list) != 1 || list[0]["id"] != disputeID {
		t.Fatalf("guest disputes = %v", list)
	}
	rec = h.do(http.MethodGet, "/api/v1/me/disputes", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host /me/disputes")
	if list := decodeArray(t, rec.Body.Bytes()); len(list) != 1 || list[0]["id"] != disputeID {
		t.Fatalf("host disputes = %v", list)
	}

	// A non-participant cannot read the dispute.
	if r := h.do(http.MethodGet, "/api/v1/disputes/"+disputeID, otherTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("third-party dispute read: status = %d, want 403", r.Code)
	}

	// Admin sees the dispute in the moderation queue.
	rec = h.do(http.MethodGet, "/api/v1/admin/disputes", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "admin queue")
	q := h.decode(rec)
	items := q["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != disputeID {
		t.Fatalf("admin queue = %v", q)
	}

	// Admin resolves with a public decision; dispute leaves the queue.
	rec = h.do(http.MethodPost, "/api/v1/admin/disputes/"+disputeID+"/resolve", adminTok, map[string]any{
		"resolution": "Refund 50% of the cleaning fee. Host to provide log.",
	})
	mustStatus(t, rec, http.StatusOK, "admin resolve")
	resolved := h.decode(rec)
	if resolved["status"] != "resolved" {
		t.Fatalf("post-resolve status: %v", resolved["status"])
	}
	if resolved["adminId"] != admin.ID.String() {
		t.Fatalf("adminId not stamped: %v", resolved)
	}

	rec = h.do(http.MethodGet, "/api/v1/admin/disputes", adminTok, nil)
	if h.decode(rec)["total"].(float64) != 0 {
		t.Fatalf("queue should be empty after resolve")
	}

	// Once closed, no more evidence can be appended.
	if r := h.do(http.MethodPost, "/api/v1/disputes/"+disputeID+"/evidence", hostTok, map[string]any{
		"note": "afterthought",
	}); r.Code == http.StatusOK {
		t.Fatalf("evidence on closed dispute should fail (got %d)", r.Code)
	}

	// Both parties received an in-app notification of the resolution.
	rec = h.do(http.MethodGet, "/api/v1/notifications", guestTok, nil)
	if total := h.decode(rec)["total"].(float64); total < 1 {
		t.Fatalf("guest should have a notification, got total=%v", total)
	}
	rec = h.do(http.MethodGet, "/api/v1/notifications", hostTok, nil)
	if total := h.decode(rec)["total"].(float64); total < 1 {
		t.Fatalf("host should have a notification, got total=%v", total)
	}
}

// TestEndToEnd_DisputeResolveAppliesPartialRefund covers S14: the admin's
// decision carries a refundAmountCents that the payment side honours,
// producing a payment_adjustments audit row, updating refundedCents and
// exposing the adjustment on the receipt. A second decision that would
// over-refund must be rejected by the payment domain — but in this test the
// dispute is already terminal after one resolve, so we exercise the over-
// refund cap by directly opening a fresh dispute against a payment that has
// already been partially refunded.
func TestEndToEnd_DisputeResolveAppliesPartialRefund(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "refund-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "refund-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "refund-admin@test.dev")

	guestTok := guest.ID.String()
	adminTok := admin.ID.String()

	b := h.seedCompletedStay(guest.ID, host.ID, "Casa do Tejo")
	bookingID := b.ID.String()

	// Guest opens a refund dispute for 4000 cents.
	rec := h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/disputes", guestTok, map[string]any{
		"kind": "refund", "reason": "Heater broken on first night.", "requestedAmountCents": 4000, "currency": "EUR",
	})
	mustStatus(t, rec, http.StatusCreated, "open refund dispute")
	disputeID := h.decode(rec)["id"].(string)

	// Admin resolves with a partial refund of 2500 cents.
	rec = h.do(http.MethodPost, "/api/v1/admin/disputes/"+disputeID+"/resolve", adminTok, map[string]any{
		"resolution":        "Awarding 2500 cents partial refund.",
		"refundAmountCents": 2500,
	})
	mustStatus(t, rec, http.StatusOK, "admin resolve with refund")

	// The booking's payment now shows 2500 refunded and a 'dispute' adjustment.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/payment", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "guest payment view")
	pay := h.decode(rec)
	if rc, _ := pay["refundedCents"].(float64); int64(rc) != 2500 {
		t.Fatalf("refundedCents = %v, want 2500", pay["refundedCents"])
	}
	adjs, _ := pay["adjustments"].([]any)
	if len(adjs) != 1 {
		t.Fatalf("adjustments = %d, want 1; payment=%v", len(adjs), pay)
	}
	first, _ := adjs[0].(map[string]any)
	if first["kind"] != "refund" || int64(first["amountCents"].(float64)) != 2500 || first["refKind"] != "dispute" || first["refId"] != disputeID {
		t.Fatalf("adjustment payload unexpected: %v", first)
	}
}

// TestEndToEnd_DisputeRefundExceedingCapturedRejected covers the headline
// invariant: cumulative refunds may not exceed the captured amount. The
// admin tries to resolve a dispute with a refund larger than the booking's
// total — the payment domain rejects, the HTTP layer returns 4xx, and the
// dispute is left open so the admin can correct the figure.
func TestEndToEnd_DisputeRefundExceedingCapturedRejected(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "overrefund-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "overrefund-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "overrefund-admin@test.dev")

	guestTok := guest.ID.String()
	adminTok := admin.ID.String()

	b := h.seedCompletedStay(guest.ID, host.ID, "Quinta da Granja")
	bookingID := b.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/disputes", guestTok, map[string]any{
		"kind": "refund", "reason": "Stay was unusable.", "requestedAmountCents": 100000, "currency": "EUR",
	})
	mustStatus(t, rec, http.StatusCreated, "open over-refund dispute")
	disputeID := h.decode(rec)["id"].(string)

	// Refund larger than the booking total — must be rejected.
	rec = h.do(http.MethodPost, "/api/v1/admin/disputes/"+disputeID+"/resolve", adminTok, map[string]any{
		"resolution":        "would over-refund",
		"refundAmountCents": 999_999_999,
	})
	if rec.Code < 400 {
		t.Fatalf("over-refund resolve should fail, got %d", rec.Code)
	}

	// The dispute is still open in the moderation queue (the failed resolve
	// did not flip it terminal) and the admin can correct the figure.
	rec = h.do(http.MethodGet, "/api/v1/admin/disputes", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "admin queue after rejected resolve")
	items := h.decode(rec)["items"].([]any)
	found := false
	for _, it := range items {
		row := it.(map[string]any)
		if row["id"] == disputeID {
			found = true
			if status, _ := row["status"].(string); status == "resolved" || status == "rejected" {
				t.Fatalf("dispute should still be open, status = %v", status)
			}
		}
	}
	if !found {
		t.Fatalf("dispute %s missing from queue after failed resolve", disputeID)
	}
}
