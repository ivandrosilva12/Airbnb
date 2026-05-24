package http_test

import (
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_PhotoManagement covers reordering (which sets the cover) and
// deleting a listing's photos, plus host-only authorization.
func TestEndToEnd_PhotoManagement(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "ph-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "ph-guest@test.dev")
	hostTok := host.ID.String()
	guestTok := guest.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Photo Loft", "type": "apartment", "city": "Faro", "country": "PT",
		"latitude": 37.0, "longitude": -7.9, "priceCents": 9000, "currency": "EUR", "maxGuests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)

	uploadPhoto(t, h, hostTok, propID)
	uploadPhoto(t, h, hostTok, propID)

	rec = h.do(http.MethodGet, "/api/v1/properties/"+propID, "", nil)
	photos := h.decode(rec)["photos"].([]any)
	if len(photos) != 2 {
		t.Fatalf("photos = %d, want 2", len(photos))
	}
	first := photos[0].(map[string]any)["id"].(string)
	second := photos[1].(map[string]any)["id"].(string)

	// Reorder so the second photo becomes the cover.
	rec = h.do(http.MethodPatch, "/api/v1/properties/"+propID+"/photos/order", hostTok, map[string]any{
		"photoIds": []string{second, first},
	})
	mustStatus(t, rec, http.StatusOK, "reorder photos")
	reordered := h.decode(rec)["photos"].([]any)
	if reordered[0].(map[string]any)["id"].(string) != second {
		t.Fatalf("cover after reorder = %v, want %s", reordered[0], second)
	}
	if pos := reordered[0].(map[string]any)["position"].(float64); pos != 0 {
		t.Fatalf("cover position = %v, want 0", pos)
	}

	// A guest cannot manage photos (host-only).
	if r := h.do(http.MethodPatch, "/api/v1/properties/"+propID+"/photos/order", guestTok, map[string]any{
		"photoIds": []string{first, second},
	}); r.Code != http.StatusForbidden {
		t.Fatalf("guest reorder: status = %d, want 403", r.Code)
	}

	// Delete the (now) cover photo; one remains and it is the other photo.
	rec = h.do(http.MethodDelete, "/api/v1/properties/"+propID+"/photos/"+second, hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "delete photo")
	remaining := h.decode(rec)["photos"].([]any)
	if len(remaining) != 1 || remaining[0].(map[string]any)["id"].(string) != first {
		t.Fatalf("after delete, photos = %v, want [%s]", remaining, first)
	}

	// Deleting a non-existent photo is a 404.
	if r := h.do(http.MethodDelete, "/api/v1/properties/"+propID+"/photos/"+second, hostTok, nil); r.Code != http.StatusNotFound {
		t.Fatalf("delete missing photo: status = %d, want 404", r.Code)
	}

	// A non-image payload is rejected by content sniffing (415), even though the
	// multipart part claims an image filename.
	if r := uploadPhotoBytes(t, h, hostTok, propID, "evil.png", []byte("<html><script>alert(1)</script>")); r.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-image upload: status = %d, want 415", r.Code)
	}

	// An oversized upload is rejected (413).
	big := make([]byte, (10<<20)+1024)
	copy(big, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	if r := uploadPhotoBytes(t, h, hostTok, propID, "big.png", big); r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload: status = %d, want 413", r.Code)
	}
}
