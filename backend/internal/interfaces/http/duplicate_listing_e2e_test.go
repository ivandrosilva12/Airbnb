package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_DuplicateListing proves S60: a host can clone an owned listing
// into a fresh draft. The copy gets a new ID, the " (copy)" suffix, the same
// pricing/amenities, and ships back in 201 ready for further edits.
func TestEndToEnd_DuplicateListing(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "dup-host@test.dev")
	tok := host.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/properties", tok, map[string]any{
		"title": "Loft Original", "type": "apartment", "city": "Porto", "country": "PT",
		"latitude": 41.15, "longitude": -8.61, "priceCents": 12000, "cleaningFeeCents": 4000,
		"currency": "EUR", "maxGuests": 4, "bedrooms": 2, "beds": 3, "bathrooms": 1,
		"amenities": []string{"wifi", "kitchen"}, "weeklyDiscountPct": 0.10,
		"instantBook": true, "minNights": 2, "maxNights": 14, "guestsIncluded": 2,
		"extraGuestFeeCents": 1000, "securityDepositCents": 20000,
	})
	mustStatus(t, rec, http.StatusCreated, "create source listing")
	src := h.decode(rec)
	srcID := src["id"].(string)

	rec = h.do(http.MethodPost, "/api/v1/properties/"+srcID+"/duplicate", tok, nil)
	mustStatus(t, rec, http.StatusCreated, "duplicate")
	dup := h.decode(rec)

	if dup["id"].(string) == srcID {
		t.Fatal("duplicate must have a fresh ID")
	}
	if got := dup["title"].(string); got != "Loft Original (copy)" {
		t.Fatalf("duplicate title = %q, want %q", got, "Loft Original (copy)")
	}
	if got := dup["status"].(string); got != "draft" {
		t.Fatalf("duplicate status = %q, want draft", got)
	}
	// Same content carries over.
	addr, _ := dup["address"].(map[string]any)
	if addr == nil || addr["city"].(string) != "Porto" {
		t.Fatalf("address.city = %v, want Porto", addr)
	}
	if got := dup["maxGuests"].(float64); got != 4 {
		t.Fatalf("maxGuests = %v, want 4", got)
	}
	if got := dup["minNights"].(float64); got != 2 {
		t.Fatalf("minNights = %v, want 2", got)
	}
	if got := dup["instantBook"].(bool); !got {
		t.Fatal("instantBook should carry over")
	}
	if got := dup["weeklyDiscountPct"].(float64); got != 0.10 {
		t.Fatalf("weeklyDiscountPct = %v", got)
	}
	// Photos NOT copied — the new listing starts photo-less.
	if photos, ok := dup["photos"].([]any); ok && len(photos) != 0 {
		t.Fatalf("duplicate should have no photos, got %d", len(photos))
	}
}

// TestEndToEnd_DuplicateListing_ForbidsOtherHost ensures another host cannot
// clone a listing they don't own (returns 403/404, not 201).
func TestEndToEnd_DuplicateListing_ForbidsOtherHost(t *testing.T) {
	h := newHarness(t)
	owner := h.seedUser(domainuser.RoleHost, "dup-owner@test.dev")
	other := h.seedUser(domainuser.RoleHost, "dup-thief@test.dev")
	ownerTok := owner.ID.String()
	otherTok := other.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/properties", ownerTok, map[string]any{
		"title": "Private Loft", "type": "apartment", "city": "Porto", "country": "PT",
		"latitude": 41.15, "longitude": -8.61, "priceCents": 10000, "cleaningFeeCents": 3000,
		"currency": "EUR", "maxGuests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create")
	srcID := h.decode(rec)["id"].(string)

	rec = h.do(http.MethodPost, "/api/v1/properties/"+srcID+"/duplicate", otherTok, nil)
	if rec.Code == http.StatusCreated {
		t.Fatalf("other host should not be able to duplicate (got 201)")
	}
}
