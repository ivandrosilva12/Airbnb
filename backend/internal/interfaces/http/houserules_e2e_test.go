package http_test

import (
	"net/http"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_HouseRules_GuestMustAcknowledgeCurrentVersion is the
// cornerstone test for S47: it walks the complete acceptance flow
// from host edit → public read → booking with wrong/missing version
// fails → booking with right version succeeds → acceptance row
// surfaces on /bookings/:id/house-rules-acceptance with the rules
// text the guest saw.
//
// If any of those steps breaks (verifier port misses, version-match
// gate inverts, post-commit recording silently drops), this test
// fails — exactly the chain the compliance promise relies on.
func TestEndToEnd_HouseRules_GuestMustAcknowledgeCurrentVersion(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "rules-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "rules-guest@test.dev")
	hostTok, guestTok := host.ID.String(), guest.ID.String()

	// Seed a published instant-book listing the guest can book.
	propID, _ := seedInstantBookListing(t, h, hostTok)

	// Initially the public read returns version 0 / empty items — no
	// acknowledgement is required at this point.
	rec := h.do(http.MethodGet, "/api/v1/properties/"+propID+"/house-rules", "", nil)
	mustStatus(t, rec, http.StatusOK, "GET rules (empty)")
	body := h.decode(rec)
	if v := int(body["version"].(float64)); v != 0 {
		t.Fatalf("initial version = %d, want 0", v)
	}
	if items := body["items"].([]any); len(items) != 0 {
		t.Fatalf("initial items = %v, want []", items)
	}

	// Host PATCHes the rules — should land at version 1.
	rec = h.do(http.MethodPatch, "/api/v1/properties/"+propID+"/house-rules", hostTok, map[string]any{
		"items": []string{"No smoking", "No parties"},
	})
	mustStatus(t, rec, http.StatusOK, "PATCH rules v1")
	if v := int(h.decode(rec)["version"].(float64)); v != 1 {
		t.Fatalf("after first PATCH, version = %d, want 1", v)
	}

	// Booking WITHOUT acknowledgement now fails — the active rule set
	// gate fires and we get 422 with a validation message.
	in := time.Now().UTC().AddDate(0, 0, 15).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 18).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	})
	mustStatus(t, rec, http.StatusUnprocessableEntity, "booking without acknowledgement should 422")

	// Booking with the WRONG version also fails — proves it's a strict
	// match, not just "any non-zero value passes".
	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
		"acceptedHouseRulesVersion": 99,
	})
	mustStatus(t, rec, http.StatusUnprocessableEntity, "booking with wrong version should 422")

	// Booking with the RIGHT version succeeds and the acceptance row
	// is recorded by the post-commit hook.
	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
		"acceptedHouseRulesVersion": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "booking with acknowledged v1")
	bookingID := h.decode(rec)["id"].(string)

	// Read the acceptance proof and confirm every field round-trips.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/house-rules-acceptance", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "GET acceptance")
	got := h.decode(rec)
	if v := int(got["acceptedVersion"].(float64)); v != 1 {
		t.Errorf("acceptedVersion = %d, want 1", v)
	}
	if got["bookingId"] != bookingID {
		t.Errorf("bookingId = %v, want %s", got["bookingId"], bookingID)
	}
	rules, _ := got["rules"].(map[string]any)
	items := rules["items"].([]any)
	if len(items) != 2 || items[0] != "No smoking" || items[1] != "No parties" {
		t.Errorf("rules.items = %v, want the version-1 list", items)
	}
}

// TestEndToEnd_HouseRules_HistoryPreservedAcrossBumps verifies the
// audit-trail promise: an acceptance recorded at version 1 still
// resolves to the version-1 text even after the host has bumped to
// version 2 with a completely different rule set. Without versioned
// persistence, a host could rewrite the rules to support a dispute.
func TestEndToEnd_HouseRules_HistoryPreservedAcrossBumps(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "history-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "history-guest@test.dev")
	hostTok, guestTok := host.ID.String(), guest.ID.String()
	propID, _ := seedInstantBookListing(t, h, hostTok)

	// v1 — strict rules.
	mustStatus(t,
		h.do(http.MethodPatch, "/api/v1/properties/"+propID+"/house-rules", hostTok, map[string]any{
			"items": []string{"No pets allowed", "Quiet hours 22:00–08:00"},
		}),
		http.StatusOK, "PATCH v1")

	// Guest books with v1 acknowledgement.
	in := time.Now().UTC().AddDate(0, 0, 20).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 22).Format("2006-01-02")
	rec := h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
		"acceptedHouseRulesVersion": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "v1 booking")
	bookingID := h.decode(rec)["id"].(string)

	// Host bumps to v2 with TOTALLY different rules — what would happen
	// in a contested dispute where the host tries to rewrite history.
	mustStatus(t,
		h.do(http.MethodPatch, "/api/v1/properties/"+propID+"/house-rules", hostTok, map[string]any{
			"items": []string{"Pets welcome", "No quiet hours"},
		}),
		http.StatusOK, "PATCH v2")

	// The acceptance still resolves to v1 text — proves history is
	// immutable, the host's edit cannot retroactively change what the
	// guest agreed to.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/house-rules-acceptance", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "GET acceptance after bump")
	got := h.decode(rec)
	if v := int(got["acceptedVersion"].(float64)); v != 1 {
		t.Errorf("acceptedVersion after bump = %d, want 1", v)
	}
	rules, _ := got["rules"].(map[string]any)
	items := rules["items"].([]any)
	if len(items) != 2 || items[0] != "No pets allowed" || items[1] != "Quiet hours 22:00–08:00" {
		t.Errorf("acceptance items after bump = %v, want the original v1 list", items)
	}
	// And the public-read endpoint reflects v2 — the two views are
	// independent: "current" advances, "acceptance" is frozen.
	rec = h.do(http.MethodGet, "/api/v1/properties/"+propID+"/house-rules", "", nil)
	mustStatus(t, rec, http.StatusOK, "GET current after bump")
	body := h.decode(rec)
	if v := int(body["version"].(float64)); v != 2 {
		t.Errorf("current version = %d, want 2", v)
	}
}

// TestEndToEnd_HouseRules_NonHostCannotEdit confirms the application
// service enforces HostID ownership — a guest who somehow reaches the
// host route group can still not edit someone else's rules.
func TestEndToEnd_HouseRules_NonHostCannotEdit(t *testing.T) {
	h := newHarness(t)
	host1 := h.seedUser(domainuser.RoleHost, "h1@test.dev")
	host2 := h.seedUser(domainuser.RoleHost, "h2@test.dev")
	host1Tok, host2Tok := host1.ID.String(), host2.ID.String()

	// host1 owns the listing.
	propID, _ := seedInstantBookListing(t, h, host1Tok)

	// host2 (different host) tries to set rules — must be 403.
	rec := h.do(http.MethodPatch, "/api/v1/properties/"+propID+"/house-rules", host2Tok, map[string]any{
		"items": []string{"hostile edit"},
	})
	mustStatus(t, rec, http.StatusForbidden, "non-owner host PATCH should be forbidden")
}

// TestEndToEnd_HouseRules_MissingAcceptance_Returns404 documents the
// shape callers must handle: a booking on a property that had no rules
// (so no acceptance was ever recorded) returns 404 on the acceptance
// endpoint, not an empty 200. This lets the dispute UI distinguish
// "no rules to acknowledge" from "rules existed but acceptance is
// missing on file".
func TestEndToEnd_HouseRules_MissingAcceptance_Returns404(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "noack-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "noack-guest@test.dev")
	hostTok, guestTok := host.ID.String(), guest.ID.String()
	propID, _ := seedInstantBookListing(t, h, hostTok)

	// Book without ever setting rules — succeeds (no gate), no acceptance row.
	in := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 32).Format("2006-01-02")
	rec := h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "booking without rules")
	bookingID := h.decode(rec)["id"].(string)

	// Acceptance endpoint must surface "not recorded" as 404 explicitly.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/house-rules-acceptance", guestTok, nil)
	mustStatus(t, rec, http.StatusNotFound, "GET acceptance for booking-without-rules should be 404")
}
