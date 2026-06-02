package http_test

import (
	"net/http"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_Fraud_BookingTriggersAssessment proves the S68 contract
// end to end: a successful booking by a fresh, unverified, soon-to-
// arrive guest produces an Assessment that the admin can browse via
// GET /admin/fraud/assessments. The booking response is unaffected
// (best-effort hook), so the guest never sees the score.
//
// Critically, the assessment must include the expected signals — so a
// future change that silently drops a rule (e.g. inlines new_account
// detection into the booking service) fails this test before shipping.
func TestEndToEnd_Fraud_BookingTriggersAssessment(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "fraud-host@test.dev")
	// Fresh guest — created right now, so the new_account rule will
	// fire when their booking lands.
	guest := h.seedUser(domainuser.RoleGuest, "fraud-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "fraud-admin@test.dev")
	hostTok, guestTok, adminTok := host.ID.String(), guest.ID.String(), admin.ID.String()

	// Seed + publish a cheap listing so the booking falls well under
	// the high-value threshold (kept off so we can assert on a
	// known signal set).
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Risk Lab", "type": "apartment", "city": "Porto", "country": "PT",
		"latitude": 41.15, "longitude": -8.61, "priceCents": 5000,
		"currency": "EUR", "maxGuests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil), http.StatusOK, "publish")

	// Short lead — check-in in 2 days, which trips the short_lead rule
	// (window is 3 days).
	in := time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 4).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "create booking")
	bookingID := h.decode(rec)["id"].(string)

	// Admin lists assessments — expect the freshly-created booking's
	// assessment to be there.
	rec = h.do(http.MethodGet, "/api/v1/admin/fraud/assessments", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "admin list")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("assessments = %d, want 1 (body: %s)", len(items), rec.Body.String())
	}
	a := items[0].(map[string]any)
	if a["bookingId"] != bookingID {
		t.Fatalf("bookingId = %v, want %s", a["bookingId"], bookingID)
	}
	score := int(a["score"].(float64))
	if score <= 0 {
		t.Fatalf("score = %d, want > 0 (new account + missing KYC + short lead should fire)", score)
	}
	// Level should be at least low. We don't pin to a specific level
	// because tweaking impact constants is in scope for future tunes
	// and would brittle-break this assertion — what we DO assert is
	// the signal names, which are the public contract.
	level := a["level"].(string)
	if level != "low" && level != "medium" && level != "high" {
		t.Fatalf("level = %s, want one of low|medium|high", level)
	}
	seenSignals := map[string]bool{}
	for _, raw := range a["signals"].([]any) {
		s := raw.(map[string]any)
		seenSignals[s["name"].(string)] = true
	}
	for _, want := range []string{"new_account", "missing_kyc", "short_lead"} {
		if !seenSignals[want] {
			t.Errorf("expected signal %q in assessment, got %v", want, seenSignals)
		}
	}

	// By-booking lookup returns the same assessment (admin booking
	// detail surface).
	rec = h.do(http.MethodGet, "/api/v1/admin/fraud/assessments/by-booking/"+bookingID, adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "by-booking lookup")
	if h.decode(rec)["bookingId"] != bookingID {
		t.Fatalf("by-booking returned wrong bookingId")
	}

	// Level filter: ?level=high should narrow. A low-risk single
	// booking will get filtered out; admin gets an empty page.
	rec = h.do(http.MethodGet, "/api/v1/admin/fraud/assessments?level=high", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "filtered list")
	// We don't assert empty (the assessment could land in high
	// depending on the random constants), but we DO assert the
	// floor: every item that DOES come back must be high.
	for _, raw := range h.decode(rec)["items"].([]any) {
		row := raw.(map[string]any)
		if row["level"] != "high" {
			t.Errorf("level filter leaked %v", row["level"])
		}
	}
}

// TestEndToEnd_Fraud_ForbidsNonAdmin keeps the read endpoints behind
// RequireAdmin — a guest must not be able to see anyone else's
// fraud trail, even their own.
func TestEndToEnd_Fraud_ForbidsNonAdmin(t *testing.T) {
	h := newHarness(t)
	guest := h.seedUser(domainuser.RoleGuest, "fraud-spy@test.dev")
	rec := h.do(http.MethodGet, "/api/v1/admin/fraud/assessments", guest.ID.String(), nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("non-admin should not list fraud assessments (got 200)")
	}
}

// TestEndToEnd_Fraud_InvalidLevelRejected — defensive check on the
// query parser so a typo in ?level=mid surfaces as 400 instead of
// silently returning everything.
func TestEndToEnd_Fraud_InvalidLevelRejected(t *testing.T) {
	h := newHarness(t)
	admin := h.seedUser(domainuser.RoleAdmin, "fraud-typo-admin@test.dev")
	rec := h.do(http.MethodGet, "/api/v1/admin/fraud/assessments?level=mid", admin.ID.String(), nil)
	mustStatus(t, rec, http.StatusBadRequest, "bad level")
}
