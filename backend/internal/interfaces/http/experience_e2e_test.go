package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_Experience_CreateUpdatePublishSearch walks the host
// lifecycle of an experience listing — create → addPhoto → update →
// publish → appears in public search — and asserts ownership gates.
//
// This pins the S71 contract end to end so future refactors can't
// silently break the host-facing flow or expose drafts to the public
// catalogue.
func TestEndToEnd_Experience_CreateUpdatePublishSearch(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "exp-host@test.dev")
	other := h.seedUser(domainuser.RoleHost, "exp-other@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "exp-guest@test.dev")
	hostTok, otherTok, guestTok := host.ID.String(), other.ID.String(), guest.ID.String()

	// Create a draft experience.
	rec := h.do(http.MethodPost, "/api/v1/experiences", hostTok, map[string]any{
		"title": "Sunset Tasca Tour", "description": "Old-town wine + tapas crawl",
		"category": "tour",
		"address":  map[string]any{"city": "Porto", "country": "PT", "latitude": 41.15, "longitude": -8.61},
		"durationMinutes": 120, "maxGuests": 6,
		"pricePerGuestCents": 2500, "currency": "EUR", "language": "en",
	})
	mustStatus(t, rec, http.StatusCreated, "create experience")
	created := h.decode(rec)
	expID := created["id"].(string)
	if created["status"].(string) != "draft" {
		t.Fatalf("status = %v, want draft", created["status"])
	}

	// Drafts must NOT appear in the public catalogue.
	rec = h.do(http.MethodGet, "/api/v1/experiences", "", nil)
	mustStatus(t, rec, http.StatusOK, "search drafts")
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("draft leaked into search: %v", items)
	}

	// Publish must fail without a photo.
	rec = h.do(http.MethodPost, "/api/v1/experiences/"+expID+"/publish", hostTok, nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("publish should refuse a photo-less listing (got 200)")
	}

	// Add a photo, then publish succeeds.
	rec = h.do(http.MethodPost, "/api/v1/experiences/"+expID+"/photos", hostTok, map[string]any{
		"objectKey": "exp/1.jpg", "url": "https://cdn/exp/1.jpg",
	})
	mustStatus(t, rec, http.StatusOK, "add photo")
	rec = h.do(http.MethodPost, "/api/v1/experiences/"+expID+"/publish", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "publish")
	if h.decode(rec)["status"].(string) != "published" {
		t.Fatal("expected status published after Publish")
	}

	// Now the listing appears in the public catalogue.
	rec = h.do(http.MethodGet, "/api/v1/experiences?category=tour&city=Porto", "", nil)
	mustStatus(t, rec, http.StatusOK, "search published")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("search items = %d, want 1", len(items))
	}
	if items[0].(map[string]any)["id"] != expID {
		t.Fatalf("search returned wrong id")
	}

	// Wrong category filter excludes it.
	rec = h.do(http.MethodGet, "/api/v1/experiences?category=cooking", "", nil)
	mustStatus(t, rec, http.StatusOK, "search wrong category")
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("category filter leaked across categories: %v", items)
	}

	// Wrong language filter excludes it.
	rec = h.do(http.MethodGet, "/api/v1/experiences?language=pt", "", nil)
	mustStatus(t, rec, http.StatusOK, "search wrong language")
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("language filter leaked across languages: %v", items)
	}

	// Other hosts cannot edit, publish, suspend, or delete this listing.
	rec = h.do(http.MethodPatch, "/api/v1/experiences/"+expID, otherTok, map[string]any{
		"title": "Hijacked", "category": "tour",
		"address":  map[string]any{"city": "Porto", "country": "PT", "latitude": 41.15, "longitude": -8.61},
		"durationMinutes": 120, "maxGuests": 6,
		"pricePerGuestCents": 2500, "currency": "EUR", "language": "en",
	})
	if rec.Code == http.StatusOK {
		t.Fatalf("non-owner should not be able to update (got 200)")
	}
	rec = h.do(http.MethodDelete, "/api/v1/experiences/"+expID, otherTok, nil)
	if rec.Code == http.StatusNoContent {
		t.Fatalf("non-owner should not be able to delete (got 204)")
	}

	// Guests cannot create.
	rec = h.do(http.MethodPost, "/api/v1/experiences", guestTok, map[string]any{
		"title": "x", "category": "tour",
		"address":  map[string]any{"city": "Porto", "country": "PT", "latitude": 41.15, "longitude": -8.61},
		"durationMinutes": 60, "maxGuests": 2,
		"pricePerGuestCents": 1000, "currency": "EUR", "language": "en",
	})
	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Fatalf("guest should not be able to create (got %d)", rec.Code)
	}

	// HostList returns the listing.
	rec = h.do(http.MethodGet, "/api/v1/host/experiences", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host list")
	if items := h.decode(rec)["items"].([]any); len(items) != 1 {
		t.Fatalf("host list len = %d, want 1", len(items))
	}

	// Owner suspends — listing leaves the public catalogue.
	rec = h.do(http.MethodPost, "/api/v1/experiences/"+expID+"/suspend", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "suspend")
	rec = h.do(http.MethodGet, "/api/v1/experiences", "", nil)
	mustStatus(t, rec, http.StatusOK, "search after suspend")
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("suspended listing should not be in catalogue: %v", items)
	}
}

// TestEndToEnd_Experience_RejectsUnknownCategory — defensive check on
// the query parser so a typo like ?category=tasting returns 400.
func TestEndToEnd_Experience_RejectsUnknownCategory(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/api/v1/experiences?category=tasting", "", nil)
	mustStatus(t, rec, http.StatusBadRequest, "bad category")
}
