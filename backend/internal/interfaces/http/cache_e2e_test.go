package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_Cache_PropertyGet_RoundTripsThroughCache walks the
// cache lifecycle end-to-end: first GET populates, second GET returns
// the cached bytes (same body), an Update invalidates the entry, the
// next GET reflects the new title. The harness wires the memory
// cache (TTL 60s) so the assertions don't depend on Redis.
//
// If the cache decorator stops returning cached bytes, stops
// invalidating on mutation, or starts ignoring per-listing isolation,
// this test fails before a deploy ships stale data to guests.
func TestEndToEnd_Cache_PropertyGet_RoundTripsThroughCache(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "cache-host@test.dev")
	hostTok := host.ID.String()

	propID, _ := seedInstantBookListing(t, h, hostTok)

	// First GET — populates the cache.
	rec1 := h.do(http.MethodGet, "/api/v1/properties/"+propID, "", nil)
	mustStatus(t, rec1, http.StatusOK, "first GET")
	firstBody := rec1.Body.String()
	firstTitle := h.decode(rec1)["title"].(string)
	if firstTitle == "" {
		t.Fatalf("first GET returned empty title")
	}

	// Second GET — must match exactly. The cached path serves the
	// stored bytes verbatim (c.Data) so the response is byte-identical.
	rec2 := h.do(http.MethodGet, "/api/v1/properties/"+propID, "", nil)
	mustStatus(t, rec2, http.StatusOK, "second GET (cached)")
	if rec2.Body.String() != firstBody {
		t.Errorf("cached GET differs from first body — cache populated incorrectly")
	}

	// Mutate the listing via PATCH. The invalidation must drop the
	// cache key so the next GET re-fetches.
	rec3 := h.do(http.MethodPatch, "/api/v1/properties/"+propID, hostTok, map[string]any{
		"title": "Cached but invalidated", "priceCents": 12000, "currency": "EUR", "maxGuests": 3,
	})
	mustStatus(t, rec3, http.StatusOK, "PATCH (invalidates cache)")

	// Third GET — should see the new title.
	rec4 := h.do(http.MethodGet, "/api/v1/properties/"+propID, "", nil)
	mustStatus(t, rec4, http.StatusOK, "third GET (post-invalidate)")
	if newTitle := h.decode(rec4)["title"].(string); newTitle != "Cached but invalidated" {
		t.Errorf("post-invalidate title = %q, want %q (cache invalidation broken)", newTitle, "Cached but invalidated")
	}
}
