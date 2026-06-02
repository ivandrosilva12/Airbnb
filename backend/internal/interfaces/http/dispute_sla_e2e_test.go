package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	domaindispute "github.com/airhost/backend/internal/domain/dispute"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// TestEndToEnd_DisputeSLAFreshDispute confirms a fresh dispute exposes
// dueAt (= openedAt + 7 days) and overdue=false in the admin queue.
func TestEndToEnd_DisputeSLAFreshDispute(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "sla-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "sla-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "sla-admin@test.dev")
	guestTok, adminTok := guest.ID.String(), admin.ID.String()

	b := h.seedCompletedStay(guest.ID, host.ID, "SLA Flat")

	rec := h.do(http.MethodPost, "/api/v1/bookings/"+b.ID.String()+"/disputes", guestTok, map[string]any{
		"kind": "refund", "reason": "lobby noisy", "requestedAmountCents": 1000, "currency": "EUR",
	})
	mustStatus(t, rec, http.StatusCreated, "open dispute")

	rec = h.do(http.MethodGet, "/api/v1/admin/disputes", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "admin list")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("queue len = %d, want 1", len(items))
	}
	view := items[0].(map[string]any)
	if view["overdue"].(bool) {
		t.Fatalf("fresh dispute reported overdue")
	}
	if view["dueAt"] == nil {
		t.Fatalf("dueAt missing on view")
	}
}

// TestEndToEnd_DisputeSLAOverdueSortsFirst seeds two open disputes — one
// fresh, one with OpenedAt rewound past the SLA window — and confirms the
// admin queue surfaces the overdue case first with overdue=true.
func TestEndToEnd_DisputeSLAOverdueSortsFirst(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "sla-host-2@test.dev")
	guestA := h.seedUser(domainuser.RoleGuest, "sla-guestA@test.dev")
	guestB := h.seedUser(domainuser.RoleGuest, "sla-guestB@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "sla-admin-2@test.dev")
	guestATok, guestBTok, adminTok := guestA.ID.String(), guestB.ID.String(), admin.ID.String()

	bA := h.seedCompletedStay(guestA.ID, host.ID, "Old Flat")
	bB := h.seedCompletedStay(guestB.ID, host.ID, "Recent Flat")

	// Open both disputes (both start fresh).
	rec := h.do(http.MethodPost, "/api/v1/bookings/"+bA.ID.String()+"/disputes", guestATok, map[string]any{
		"kind": "refund", "reason": "wifi broken", "requestedAmountCents": 1000, "currency": "EUR",
	})
	mustStatus(t, rec, http.StatusCreated, "open A")
	oldDisputeID := h.decode(rec)["id"].(string)

	rec = h.do(http.MethodPost, "/api/v1/bookings/"+bB.ID.String()+"/disputes", guestBTok, map[string]any{
		"kind": "refund", "reason": "smelly", "requestedAmountCents": 2000, "currency": "EUR",
	})
	mustStatus(t, rec, http.StatusCreated, "open B")
	recentDisputeID := h.decode(rec)["id"].(string)
	_ = recentDisputeID

	// Backdate the first dispute so it's now overdue. We mutate through the
	// dispute repo directly via the harness — the public API doesn't expose
	// "open in the past" deliberately.
	oldID, _ := uuid.Parse(oldDisputeID)
	rewindDisputeOpenedAt(t, h, oldID, time.Now().Add(-domaindispute.SLAWindow-time.Hour))

	// Queue should list the overdue one first with overdue=true.
	rec = h.do(http.MethodGet, "/api/v1/admin/disputes", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "queue")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("queue len = %d, want 2", len(items))
	}
	first := items[0].(map[string]any)
	if first["id"] != oldDisputeID {
		t.Fatalf("first item id = %v, want overdue %s", first["id"], oldDisputeID)
	}
	if !first["overdue"].(bool) {
		t.Fatalf("first item overdue = false, want true (body: %s)", rec.Body.String())
	}
	if items[1].(map[string]any)["overdue"].(bool) {
		t.Fatalf("recent item should not be overdue")
	}
}

// TestEndToEnd_DisputeSLATerminalNotOverdue confirms a resolved dispute,
// even one decided after the SLA, reports overdue=false (the moderator's
// clock stopped at the decision).
func TestEndToEnd_DisputeSLATerminalNotOverdue(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "sla-term-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "sla-term-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "sla-term-admin@test.dev")
	guestTok, adminTok := guest.ID.String(), admin.ID.String()

	b := h.seedCompletedStay(guest.ID, host.ID, "Terminal Flat")
	rec := h.do(http.MethodPost, "/api/v1/bookings/"+b.ID.String()+"/disputes", guestTok, map[string]any{
		"kind": "refund", "reason": "late check-in", "requestedAmountCents": 1000, "currency": "EUR",
	})
	mustStatus(t, rec, http.StatusCreated, "open")
	disputeID := h.decode(rec)["id"].(string)
	dID, _ := uuid.Parse(disputeID)

	// Backdate it past the SLA, then admin rejects.
	rewindDisputeOpenedAt(t, h, dID, time.Now().Add(-domaindispute.SLAWindow-24*time.Hour))
	rec = h.do(http.MethodPost, "/api/v1/admin/disputes/"+disputeID+"/reject", adminTok, map[string]any{
		"resolution": "no evidence",
	})
	mustStatus(t, rec, http.StatusOK, "reject")

	// After rejection, queue no longer surfaces it as open (the queue
	// returns only open/under_review). Sanity-check via /me/disputes which
	// includes terminal entries, asserting overdue=false on the view.
	rec = h.do(http.MethodGet, "/api/v1/me/disputes", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "my disputes")
	// /me/disputes returns a JSON array directly (no items wrapper), so we
	// decode straight into a slice.
	var items []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode /me/disputes: %v (body: %s)", err, rec.Body.String())
	}
	if len(items) != 1 {
		t.Fatalf("my disputes len = %d, want 1", len(items))
	}
	view := items[0]
	if view["status"] != "rejected" {
		t.Fatalf("status = %v, want rejected", view["status"])
	}
	if view["overdue"].(bool) {
		t.Fatalf("terminal dispute reported overdue")
	}
}

// rewindDisputeOpenedAt cheats into the in-memory store to backdate the
// OpenedAt timestamp. Used only in tests to drive the SLA gate; the public
// API never lets a caller pick OpenedAt.
func rewindDisputeOpenedAt(t *testing.T, h *harness, id uuid.UUID, when time.Time) {
	t.Helper()
	d, err := h.disputeRepo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("find dispute: %v", err)
	}
	d.OpenedAt = when.UTC()
	if err := h.disputeRepo.Save(context.Background(), d); err != nil {
		t.Fatalf("save dispute: %v", err)
	}
}
