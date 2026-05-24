package http_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/shared"
	domainuser "github.com/airhost/backend/internal/domain/user"
)

// memSilencer is an in-memory port.AlertSilencer for the e2e harness, standing
// in for a real Alertmanager so the admin silence endpoints can be exercised.
type memSilencer struct {
	mu     sync.Mutex
	seq    int
	byID   map[string]port.Silence
	listed []string
}

func newMemSilencer() *memSilencer {
	return &memSilencer{byID: map[string]port.Silence{}}
}

func (m *memSilencer) Create(_ context.Context, s port.NewSilence) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id := fmt.Sprintf("sil-%d", m.seq)
	m.byID[id] = port.Silence{
		ID:        id,
		Matchers:  s.Matchers,
		StartsAt:  s.StartsAt,
		EndsAt:    s.EndsAt,
		CreatedBy: s.CreatedBy,
		Comment:   s.Comment,
		Status:    "active",
	}
	m.listed = append(m.listed, id)
	return id, nil
}

func (m *memSilencer) List(context.Context) ([]port.Silence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]port.Silence, 0, len(m.listed))
	for _, id := range m.listed {
		out = append(out, m.byID[id])
	}
	return out, nil
}

func (m *memSilencer) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[id]; !ok {
		return fmt.Errorf("not found: %w", shared.ErrNotFound)
	}
	delete(m.byID, id)
	for i, x := range m.listed {
		if x == id {
			m.listed = append(m.listed[:i], m.listed[i+1:]...)
			break
		}
	}
	return nil
}

// TestEndToEnd_AlertSilences covers the admin silence flow: create a maintenance
// window, list it, delete it, and the authorization + validation guards.
func TestEndToEnd_AlertSilences(t *testing.T) {
	h := newHarness(t)
	guest := h.seedUser(domainuser.RoleGuest, "sil-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "sil-admin@test.dev")
	guestTok := guest.ID.String()
	adminTok := admin.ID.String()

	// A non-admin cannot manage silences.
	if r := h.do(http.MethodGet, "/api/v1/admin/alerts/silences", guestTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("guest list silences: status = %d, want 403", r.Code)
	}
	if r := h.do(http.MethodPost, "/api/v1/admin/alerts/silences", guestTok, map[string]any{
		"matchers":        []map[string]any{{"name": "alertname", "value": "AirhostApiDown"}},
		"durationMinutes": 60, "comment": "x",
	}); r.Code != http.StatusForbidden {
		t.Fatalf("guest create silence: status = %d, want 403", r.Code)
	}

	// Validation: no matchers is a 422.
	if r := h.do(http.MethodPost, "/api/v1/admin/alerts/silences", adminTok, map[string]any{
		"durationMinutes": 60, "comment": "x",
	}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("no matchers: status = %d, want 422", r.Code)
	}
	// Validation: missing comment is a 422.
	if r := h.do(http.MethodPost, "/api/v1/admin/alerts/silences", adminTok, map[string]any{
		"matchers":        []map[string]any{{"name": "alertname", "value": "AirhostApiDown"}},
		"durationMinutes": 60,
	}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("no comment: status = %d, want 422", r.Code)
	}

	// Admin creates a 2-hour silence on the noisy webhook-rejection warning.
	rec := h.do(http.MethodPost, "/api/v1/admin/alerts/silences", adminTok, map[string]any{
		"matchers": []map[string]any{
			{"name": "alertname", "value": "AirhostWebhookRejectionSpike"},
			{"name": "provider", "value": "stripe"},
		},
		"durationMinutes": 120,
		"comment":         "Rotating Stripe webhook secret",
	})
	mustStatus(t, rec, http.StatusCreated, "create silence")
	body := h.decode(rec)
	silenceID := body["id"].(string)
	if silenceID == "" {
		t.Fatal("expected a silence id")
	}
	if body["status"].(string) != "active" {
		t.Fatalf("status = %v, want active", body["status"])
	}
	if body["createdBy"].(string) != "sil-admin@test.dev" {
		t.Fatalf("createdBy = %v, want the admin email", body["createdBy"])
	}

	// It appears in the list.
	rec = h.do(http.MethodGet, "/api/v1/admin/alerts/silences", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "list silences")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("silence items = %d, want 1", len(items))
	}

	// Admin deletes it.
	rec = h.do(http.MethodDelete, "/api/v1/admin/alerts/silences/"+silenceID, adminTok, nil)
	mustStatus(t, rec, http.StatusNoContent, "delete silence")

	// Deleting a missing silence is a 404.
	if r := h.do(http.MethodDelete, "/api/v1/admin/alerts/silences/does-not-exist", adminTok, nil); r.Code != http.StatusNotFound {
		t.Fatalf("delete missing: status = %d, want 404", r.Code)
	}

	// The list is empty again.
	rec = h.do(http.MethodGet, "/api/v1/admin/alerts/silences", adminTok, nil)
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("silences after delete = %d, want 0", len(items))
	}
}

// TestEndToEnd_AlertState covers the Alertmanager webhook receiver feeding the
// admin alert-state view: a firing notification appears, then a resolved one
// flips its status (so the internal UI reflects resolved, not just email/Slack).
func TestEndToEnd_AlertState(t *testing.T) {
	h := newHarness(t)
	guest := h.seedUser(domainuser.RoleGuest, "as-guest@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "as-admin@test.dev")

	// Initially there are no alerts.
	rec := h.do(http.MethodGet, "/api/v1/admin/alerts", admin.ID.String(), nil)
	mustStatus(t, rec, http.StatusOK, "list alerts (empty)")
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("alerts initially = %d, want 0", len(items))
	}

	// Alertmanager posts a firing notification (no user token — internal route).
	firing := map[string]any{
		"status": "firing",
		"alerts": []map[string]any{{
			"status":      "firing",
			"fingerprint": "fp-1",
			"labels":      map[string]string{"alertname": "AirhostApiDown", "severity": "critical"},
			"annotations": map[string]string{"summary": "AirHost API is down", "runbook_url": "http://rb/AirhostApiDown"},
			"startsAt":    "2026-05-24T12:00:00Z",
		}},
	}
	rec = h.do(http.MethodPost, "/api/v1/webhooks/alerts", "", firing)
	mustStatus(t, rec, http.StatusOK, "ingest firing")

	// The admin sees it as firing, with the runbook link.
	rec = h.do(http.MethodGet, "/api/v1/admin/alerts", admin.ID.String(), nil)
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("alerts after firing = %d, want 1", len(items))
	}
	first := items[0].(map[string]any)
	if first["status"].(string) != "firing" || first["alertName"].(string) != "AirhostApiDown" {
		t.Fatalf("unexpected alert: %+v", first)
	}
	if first["runbookUrl"].(string) != "http://rb/AirhostApiDown" {
		t.Fatalf("runbookUrl = %v", first["runbookUrl"])
	}

	// A non-admin cannot read the alert-state view.
	if r := h.do(http.MethodGet, "/api/v1/admin/alerts", guest.ID.String(), nil); r.Code != http.StatusForbidden {
		t.Fatalf("guest list alerts: status = %d, want 403", r.Code)
	}

	// Alertmanager posts the resolved notification for the same fingerprint.
	resolved := map[string]any{
		"status": "resolved",
		"alerts": []map[string]any{{
			"status":      "resolved",
			"fingerprint": "fp-1",
			"labels":      map[string]string{"alertname": "AirhostApiDown", "severity": "critical"},
			"annotations": map[string]string{"summary": "AirHost API is down"},
			"startsAt":    "2026-05-24T12:00:00Z",
			"endsAt":      "2026-05-24T12:05:00Z",
		}},
	}
	rec = h.do(http.MethodPost, "/api/v1/webhooks/alerts", "", resolved)
	mustStatus(t, rec, http.StatusOK, "ingest resolved")

	// The state flips to resolved (still one entry, not duplicated).
	rec = h.do(http.MethodGet, "/api/v1/admin/alerts", admin.ID.String(), nil)
	items = h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("alerts after resolve = %d, want 1", len(items))
	}
	if status := items[0].(map[string]any)["status"].(string); status != "resolved" {
		t.Fatalf("status = %q, want resolved", status)
	}
}
