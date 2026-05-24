package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_IdentityVerification covers the KYC flow: a user submits a
// document, an admin reviews the queue and approves it, the user sees a
// verified status, and a notification + account email are produced. It also
// checks the resubmission rules and admin-only authorization.
func TestEndToEnd_IdentityVerification(t *testing.T) {
	h := newHarness(t)
	guest := h.seedUser(domainuser.RoleGuest, "kyc-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "kyc-admin@test.dev")
	guestTok := guest.ID.String()
	adminTok := admin.ID.String()

	// Initially unverified: GET returns 200 with a null verification.
	rec := h.do(http.MethodGet, "/api/v1/me/verification", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "get verification (none)")
	if v := h.decode(rec)["verification"]; v != nil {
		t.Fatalf("expected null verification, got %v", v)
	}

	// Submit a document.
	rec = h.do(http.MethodPost, "/api/v1/me/verification", guestTok, map[string]any{
		"documentType": "passport", "documentRef": "scan-key-1", "legalName": "Grace Hopper",
	})
	mustStatus(t, rec, http.StatusCreated, "submit verification")
	if status := h.decode(rec)["status"].(string); status != "pending" {
		t.Fatalf("status = %q, want pending", status)
	}

	// Submitting again while pending is rejected (a domain validation error).
	if r := h.do(http.MethodPost, "/api/v1/me/verification", guestTok, map[string]any{
		"documentType": "id_card", "documentRef": "scan-key-2", "legalName": "Grace Hopper",
	}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate submit: status = %d, want 422", r.Code)
	}

	// A non-admin cannot see the review queue.
	if r := h.do(http.MethodGet, "/api/v1/admin/verifications", guestTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("guest queue: status = %d, want 403", r.Code)
	}

	// Admin sees the pending request.
	rec = h.do(http.MethodGet, "/api/v1/admin/verifications", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "admin queue")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("queue items = %d, want 1", len(items))
	}
	vID := items[0].(map[string]any)["id"].(string)

	// Admin approves it.
	rec = h.do(http.MethodPost, "/api/v1/admin/verifications/"+vID+"/approve", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "approve verification")
	if status := h.decode(rec)["status"].(string); status != "approved" {
		t.Fatalf("status = %q, want approved", status)
	}

	// The user now reads an approved verification.
	rec = h.do(http.MethodGet, "/api/v1/me/verification", guestTok, nil)
	v := h.decode(rec)["verification"].(map[string]any)
	if v["status"].(string) != "approved" {
		t.Fatalf("user verification status = %v, want approved", v["status"])
	}

	// Approval produced an in-app notification for the user.
	rec = h.do(http.MethodGet, "/api/v1/notifications", guestTok, nil)
	notifs := h.decode(rec)["items"].([]any)
	found := false
	for _, n := range notifs {
		if n.(map[string]any)["type"].(string) == "identity_verified" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an identity_verified notification, got %v", notifs)
	}

	// And an account email was sent to the user (always delivered, not opt-out).
	gotEmail := false
	for _, m := range h.mailer.Sent() {
		if m.To == guest.Email && m.Subject == "Identity verified" {
			gotEmail = true
		}
	}
	if !gotEmail {
		t.Fatalf("expected an 'Identity verified' email to the user, sent: %+v", h.mailer.Sent())
	}

	// Submitting again once verified is rejected (a domain validation error).
	if r := h.do(http.MethodPost, "/api/v1/me/verification", guestTok, map[string]any{
		"documentType": "passport", "documentRef": "scan-key-3", "legalName": "Grace Hopper",
	}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("submit when verified: status = %d, want 422", r.Code)
	}
}
