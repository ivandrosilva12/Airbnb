package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_AdminSuspendsUser proves the S61 admin suspension flow end to
// end: admin suspends → the user's bearer is rejected by the auth middleware
// → admin unsuspends → the user can act again. Also asserts the audit trail
// captures both transitions with the right action/target/metadata.
func TestEndToEnd_AdminSuspendsUser(t *testing.T) {
	h := newHarness(t)
	admin := h.seedUser(domainuser.RoleAdmin, "sus-admin@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "sus-guest@test.dev")
	adminTok := admin.ID.String()
	guestTok := guest.ID.String()

	// Sanity: before suspension the guest can read /me.
	mustStatus(t, h.do(http.MethodGet, "/api/v1/me", guestTok, nil), http.StatusOK, "guest /me pre-suspend")

	// Suspend.
	rec := h.do(http.MethodPost, "/api/v1/admin/users/"+guest.ID.String()+"/suspend", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "admin suspend")
	if active := h.decode(rec)["isActive"].(bool); active {
		t.Fatal("user view should report isActive=false after suspend")
	}

	// The guest's bearer is now refused by the auth middleware (403).
	rec = h.do(http.MethodGet, "/api/v1/me", guestTok, nil)
	mustStatus(t, rec, http.StatusForbidden, "guest /me while suspended")

	// Unsuspend.
	rec = h.do(http.MethodPost, "/api/v1/admin/users/"+guest.ID.String()+"/unsuspend", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "admin unsuspend")
	if active := h.decode(rec)["isActive"].(bool); !active {
		t.Fatal("user view should report isActive=true after unsuspend")
	}

	// Guest can act again.
	mustStatus(t, h.do(http.MethodGet, "/api/v1/me", guestTok, nil), http.StatusOK, "guest /me post-unsuspend")

	// Audit trail: two rows, both keyed by the target user. Sort order
	// is not asserted — two CreatedAt values within the same millisecond
	// are unordered (sort.Slice is not stable). We only care that both
	// transitions landed in the trail with the right shape.
	rec = h.do(http.MethodGet, "/api/v1/admin/audit?targetType=user&targetId="+guest.ID.String(), adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "audit list")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("audit rows = %d, want 2 (body: %s)", len(items), rec.Body.String())
	}
	seen := map[string]bool{}
	for _, raw := range items {
		row := raw.(map[string]any)
		if row["targetType"] != "user" {
			t.Fatalf("targetType = %v, want user", row["targetType"])
		}
		if row["targetId"] != guest.ID.String() {
			t.Fatalf("targetId = %v, want %s", row["targetId"], guest.ID.String())
		}
		if row["actorId"] != admin.ID.String() {
			t.Fatalf("actorId = %v, want admin %s", row["actorId"], admin.ID.String())
		}
		if meta, _ := row["metadata"].(map[string]any); meta["email"] != "sus-guest@test.dev" {
			t.Fatalf("metadata.email = %v, want sus-guest@test.dev", meta["email"])
		}
		seen[row["action"].(string)] = true
	}
	if !seen["user.suspend"] || !seen["user.unsuspend"] {
		t.Fatalf("expected both suspend+unsuspend in audit, got %v", seen)
	}
}

// TestEndToEnd_AdminCannotSuspendSelf — defensive check: an admin's own ID is
// rejected with 400 so a misclick can't lock themselves out of the platform.
func TestEndToEnd_AdminCannotSuspendSelf(t *testing.T) {
	h := newHarness(t)
	admin := h.seedUser(domainuser.RoleAdmin, "sus-selfadmin@test.dev")
	adminTok := admin.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/admin/users/"+admin.ID.String()+"/suspend", adminTok, nil)
	mustStatus(t, rec, http.StatusBadRequest, "self-suspend")
}

// TestEndToEnd_SuspendForbidsNonAdmin — the admin route gate must reject a
// regular user trying to escalate (403 from RequireAdmin).
func TestEndToEnd_SuspendForbidsNonAdmin(t *testing.T) {
	h := newHarness(t)
	guest := h.seedUser(domainuser.RoleGuest, "sus-attacker@test.dev")
	victim := h.seedUser(domainuser.RoleGuest, "sus-victim@test.dev")
	guestTok := guest.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/admin/users/"+victim.ID.String()+"/suspend", guestTok, nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("non-admin should not be able to suspend (got 200)")
	}
}
