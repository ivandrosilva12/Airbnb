package http_test

import (
	"net/http"
	"strings"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_MessageTemplateCRUD walks an author through the playbook
// lifecycle: create, list, update, delete.
func TestEndToEnd_MessageTemplateCRUD(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "tpl-host@test.dev")
	hostTok := host.ID.String()

	// Initially empty.
	rec := h.do(http.MethodGet, "/api/v1/me/message-templates", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "list empty")
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("empty list = %d, want 0", len(items))
	}

	// Create a template.
	rec = h.do(http.MethodPost, "/api/v1/me/message-templates", hostTok, map[string]any{
		"label": "Check-in",
		"body":  "Hi! Check-in is from 3pm. Lockbox code: 1234.",
	})
	mustStatus(t, rec, http.StatusCreated, "create template")
	created := h.decode(rec)
	tplID := created["id"].(string)
	if created["label"] != "Check-in" {
		t.Fatalf("created label = %v, want Check-in", created["label"])
	}

	// Create a second one — list returns them sorted by label asc.
	rec = h.do(http.MethodPost, "/api/v1/me/message-templates", hostTok, map[string]any{
		"label": "Arrival info",
		"body":  "Welcome! Your wifi password is on the table.",
	})
	mustStatus(t, rec, http.StatusCreated, "create second template")

	rec = h.do(http.MethodGet, "/api/v1/me/message-templates", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "list after creates")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("list len = %d, want 2", len(items))
	}
	if items[0].(map[string]any)["label"] != "Arrival info" {
		t.Fatalf("sort wrong; first label = %v, want 'Arrival info'", items[0].(map[string]any)["label"])
	}

	// Update the first one.
	rec = h.do(http.MethodPatch, "/api/v1/me/message-templates/"+tplID, hostTok, map[string]any{
		"label": "Check-in & Wi-Fi",
		"body":  "Check-in 3pm. Lockbox 1234. Wi-Fi: SeaView / pass2024.",
	})
	mustStatus(t, rec, http.StatusOK, "update template")
	if h.decode(rec)["label"] != "Check-in & Wi-Fi" {
		t.Fatalf("update label not persisted")
	}

	// Delete.
	if r := h.do(http.MethodDelete, "/api/v1/me/message-templates/"+tplID, hostTok, nil); r.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", r.Code)
	}

	// One left.
	rec = h.do(http.MethodGet, "/api/v1/me/message-templates", hostTok, nil)
	if items := h.decode(rec)["items"].([]any); len(items) != 1 {
		t.Fatalf("after delete list = %d, want 1", len(items))
	}
}

// TestEndToEnd_MessageTemplateValidation confirms label/body bounds enforced
// by the aggregate surface as 422.
func TestEndToEnd_MessageTemplateValidation(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "tpl-val@test.dev")
	hostTok := host.ID.String()

	cases := []map[string]any{
		{"label": "", "body": "anything"},                                    // missing label (binding required)
		{"label": "ok", "body": ""},                                          // missing body
		{"label": strings.Repeat("x", 61), "body": "ok"},                     // label too long
		{"label": "ok", "body": strings.Repeat("y", 4001)},                   // body too long
	}
	for i, body := range cases {
		r := h.do(http.MethodPost, "/api/v1/me/message-templates", hostTok, body)
		// Missing required fields return 400 from binding; length violations 422.
		if r.Code != http.StatusBadRequest && r.Code != http.StatusUnprocessableEntity {
			t.Fatalf("case %d: status = %d, want 400 or 422 (body: %s)", i, r.Code, r.Body.String())
		}
	}
}

// TestEndToEnd_MessageTemplateOwnerOnly confirms a non-owner cannot read,
// update or delete another host's templates.
func TestEndToEnd_MessageTemplateOwnerOnly(t *testing.T) {
	h := newHarness(t)
	owner := h.seedUser(domainuser.RoleHost, "tpl-owner@test.dev")
	other := h.seedUser(domainuser.RoleHost, "tpl-other@test.dev")
	ownerTok, otherTok := owner.ID.String(), other.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/me/message-templates", ownerTok, map[string]any{
		"label": "private", "body": "owner's template",
	})
	mustStatus(t, rec, http.StatusCreated, "owner create")
	tplID := h.decode(rec)["id"].(string)

	// Other host's list is empty (per-user scoping).
	rec = h.do(http.MethodGet, "/api/v1/me/message-templates", otherTok, nil)
	mustStatus(t, rec, http.StatusOK, "other list")
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("other host list = %d, want 0", len(items))
	}

	// Other host cannot update.
	if r := h.do(http.MethodPatch, "/api/v1/me/message-templates/"+tplID, otherTok, map[string]any{
		"label": "hijacked", "body": "x",
	}); r.Code != http.StatusForbidden {
		t.Fatalf("other update: status = %d, want 403", r.Code)
	}
	// Nor delete.
	if r := h.do(http.MethodDelete, "/api/v1/me/message-templates/"+tplID, otherTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("other delete: status = %d, want 403", r.Code)
	}
}
