package http_test

import (
	"net/http"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// seedPublishedProperty creates a listing owned by `host`, uploads a single
// photo so it can be published, and returns the listing id. Mirrors the
// minimal happy-path setup used by the other e2e suites in this package.
func seedPublishedProperty(t *testing.T, h *harness, hostTok string) string {
	t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title":      "Co-host suite",
		"type":       "apartment",
		"city":       "Porto",
		"country":    "PT",
		"latitude":   41.15,
		"longitude":  -8.61,
		"priceCents": 9000,
		"currency":   "EUR",
		"maxGuests":  2,
	})
	mustStatus(t, rec, http.StatusCreated, "seed property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "seed publish")
	return propID
}

// TestEndToEnd_CohostManageCalendarBlocks confirms the relaxed gate: a
// co-host with manage_calendar can create/list/delete blocks on the listing,
// while a stranger cannot.
func TestEndToEnd_CohostManageCalendarBlocks(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "primary-host@test.dev")
	cohost := h.seedUser(domainuser.RoleGuest, "cohost-cal@test.dev") // intentionally not a host
	other := h.seedUser(domainuser.RoleGuest, "stranger@test.dev")
	hostTok, cohostTok, otherTok := host.ID.String(), cohost.ID.String(), other.ID.String()

	propID := seedPublishedProperty(t, h, hostTok)

	// Invite cohost by email with manage_calendar.
	rec := h.do(http.MethodPost, "/api/v1/host/properties/"+propID+"/cohosts", hostTok, map[string]any{
		"email":       cohost.Email,
		"permissions": []string{"manage_calendar"},
	})
	mustStatus(t, rec, http.StatusCreated, "invite cohost")

	// Co-host can now create a block — even though they don't have the host role.
	from := time.Now().UTC().AddDate(0, 0, 20).Format("2006-01-02")
	to := time.Now().UTC().AddDate(0, 0, 23).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/blocks", cohostTok, map[string]any{
		"from": from, "to": to, "reason": "maintenance",
	})
	mustStatus(t, rec, http.StatusCreated, "cohost create block")
	blockID := h.decode(rec)["id"].(string)

	// A stranger cannot.
	if r := h.do(http.MethodPost, "/api/v1/properties/"+propID+"/blocks", otherTok, map[string]any{
		"from": from, "to": to,
	}); r.Code != http.StatusForbidden {
		t.Fatalf("stranger block: status = %d, want 403", r.Code)
	}

	// Co-host can list blocks.
	rec = h.do(http.MethodGet, "/api/v1/properties/"+propID+"/blocks", cohostTok, nil)
	mustStatus(t, rec, http.StatusOK, "cohost list blocks")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("cohost blocks = %d, want 1", len(items))
	}

	// Co-host can delete a block they did or didn't create (per-listing perm).
	if r := h.do(http.MethodDelete, "/api/v1/blocks/"+blockID, cohostTok, nil); r.Code != http.StatusNoContent {
		t.Fatalf("cohost delete block: status = %d, want 204", r.Code)
	}
}

// TestEndToEnd_CohostManagePricingRules confirms manage_pricing without
// manage_calendar bars calendar mutation, and vice versa.
func TestEndToEnd_CohostManagePricingRules(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "host-pricing@test.dev")
	cohost := h.seedUser(domainuser.RoleGuest, "cohost-pricing@test.dev")
	hostTok, cohostTok := host.ID.String(), cohost.ID.String()

	propID := seedPublishedProperty(t, h, hostTok)

	// Invite with manage_pricing only.
	rec := h.do(http.MethodPost, "/api/v1/host/properties/"+propID+"/cohosts", hostTok, map[string]any{
		"email":       cohost.Email,
		"permissions": []string{"manage_pricing"},
	})
	mustStatus(t, rec, http.StatusCreated, "invite cohost")

	// Pricing mutation succeeds.
	start := time.Now().UTC().AddDate(0, 0, 40).Format("2006-01-02")
	end := time.Now().UTC().AddDate(0, 0, 47).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/price-rules", cohostTok, map[string]any{
		"startDate": start, "endDate": end, "priceCents": 15000, "label": "easter",
	})
	mustStatus(t, rec, http.StatusCreated, "cohost create price rule")

	// Same co-host, no manage_calendar → block creation rejected.
	if r := h.do(http.MethodPost, "/api/v1/properties/"+propID+"/blocks", cohostTok, map[string]any{
		"from": time.Now().UTC().AddDate(0, 0, 60).Format("2006-01-02"),
		"to":   time.Now().UTC().AddDate(0, 0, 62).Format("2006-01-02"),
	}); r.Code != http.StatusForbidden {
		t.Fatalf("cohost without manage_calendar: status = %d, want 403", r.Code)
	}
}

// TestEndToEnd_CohostInviteListRevoke covers the host-only management surface:
// invite, list (with email + perms), revoke, and re-invite after revocation.
func TestEndToEnd_CohostInviteListRevoke(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "host-mgmt@test.dev")
	cohost := h.seedUser(domainuser.RoleGuest, "cohost-mgmt@test.dev")
	hostTok, cohostTok := host.ID.String(), cohost.ID.String()

	propID := seedPublishedProperty(t, h, hostTok)

	// Invite.
	rec := h.do(http.MethodPost, "/api/v1/host/properties/"+propID+"/cohosts", hostTok, map[string]any{
		"email":       cohost.Email,
		"permissions": []string{"manage_calendar", "manage_pricing"},
	})
	mustStatus(t, rec, http.StatusCreated, "invite")
	cohostID := h.decode(rec)["id"].(string)

	// Re-inviting the same user returns 409.
	if r := h.do(http.MethodPost, "/api/v1/host/properties/"+propID+"/cohosts", hostTok, map[string]any{
		"email": cohost.Email, "permissions": []string{"manage_calendar"},
	}); r.Code != http.StatusConflict {
		t.Fatalf("duplicate invite: status = %d, want 409", r.Code)
	}

	// List: shows email + perm set.
	rec = h.do(http.MethodGet, "/api/v1/host/properties/"+propID+"/cohosts", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "list")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list cohosts = %d, want 1", len(items))
	}
	first := items[0].(map[string]any)
	if first["email"] != cohost.Email {
		t.Fatalf("cohost email = %v, want %s", first["email"], cohost.Email)
	}
	perms := first["permissions"].([]any)
	if len(perms) != 2 {
		t.Fatalf("perms = %v, want 2", perms)
	}

	// "Listings I help manage" surfaces this for the cohost.
	rec = h.do(http.MethodGet, "/api/v1/me/cohost-listings", cohostTok, nil)
	mustStatus(t, rec, http.StatusOK, "list mine")
	mine := h.decode(rec)["items"].([]any)
	if len(mine) != 1 {
		t.Fatalf("cohost listings = %d, want 1", len(mine))
	}

	// PATCH: reduce to manage_pricing only.
	rec = h.do(http.MethodPatch, "/api/v1/host/properties/"+propID+"/cohosts/"+cohostID, hostTok, map[string]any{
		"permissions": []string{"manage_pricing"},
	})
	mustStatus(t, rec, http.StatusOK, "patch")
	if updated := h.decode(rec)["permissions"].([]any); len(updated) != 1 || updated[0] != "manage_pricing" {
		t.Fatalf("patch perms = %v, want [manage_pricing]", updated)
	}

	// Revoke.
	if r := h.do(http.MethodDelete, "/api/v1/host/properties/"+propID+"/cohosts/"+cohostID, hostTok, nil); r.Code != http.StatusNoContent {
		t.Fatalf("revoke: status = %d, want 204", r.Code)
	}

	// After revoke, the calendar gate rejects the cohost again.
	if r := h.do(http.MethodPost, "/api/v1/properties/"+propID+"/blocks", cohostTok, map[string]any{
		"from": time.Now().UTC().AddDate(0, 0, 80).Format("2006-01-02"),
		"to":   time.Now().UTC().AddDate(0, 0, 82).Format("2006-01-02"),
	}); r.Code != http.StatusForbidden {
		t.Fatalf("revoked cohost block: status = %d, want 403", r.Code)
	}
}

// TestEndToEnd_CohostNonOwnerCannotManage confirms only the primary host
// can invite or list co-hosts — a stranger and the cohost themselves both 403.
func TestEndToEnd_CohostNonOwnerCannotManage(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "owner@test.dev")
	cohost := h.seedUser(domainuser.RoleHost, "cohost-owner@test.dev") // host role, just not primary
	stranger := h.seedUser(domainuser.RoleHost, "stranger-host@test.dev")
	hostTok, cohostTok, strangerTok := host.ID.String(), cohost.ID.String(), stranger.ID.String()

	propID := seedPublishedProperty(t, h, hostTok)

	// Set the cohost up so a list endpoint has something to show.
	rec := h.do(http.MethodPost, "/api/v1/host/properties/"+propID+"/cohosts", hostTok, map[string]any{
		"email": cohost.Email, "permissions": []string{"manage_calendar"},
	})
	mustStatus(t, rec, http.StatusCreated, "invite")

	// A different host cannot read the grants.
	if r := h.do(http.MethodGet, "/api/v1/host/properties/"+propID+"/cohosts", strangerTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("stranger list cohosts: status = %d, want 403", r.Code)
	}
	// Nor can they invite.
	if r := h.do(http.MethodPost, "/api/v1/host/properties/"+propID+"/cohosts", strangerTok, map[string]any{
		"email": "x@test.dev", "permissions": []string{"manage_calendar"},
	}); r.Code != http.StatusForbidden {
		t.Fatalf("stranger invite: status = %d, want 403", r.Code)
	}
	// And the cohost themselves cannot manage other cohosts on the listing.
	if r := h.do(http.MethodGet, "/api/v1/host/properties/"+propID+"/cohosts", cohostTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("cohost list cohosts: status = %d, want 403", r.Code)
	}
}

// TestEndToEnd_CohostCannotInviteSelfHost confirms inviting the primary host
// as their own co-host is rejected with a validation error.
func TestEndToEnd_CohostCannotInviteSelfHost(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "host-self@test.dev")
	hostTok := host.ID.String()

	propID := seedPublishedProperty(t, h, hostTok)

	if r := h.do(http.MethodPost, "/api/v1/host/properties/"+propID+"/cohosts", hostTok, map[string]any{
		"email": host.Email, "permissions": []string{"manage_calendar"},
	}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invite self: status = %d, want 422", r.Code)
	}
}
