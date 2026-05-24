package http_test

import (
	"net/http"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_AdvancedPricing verifies that a listing's weekly discount and tax
// flow into a booking's price breakdown for a qualifying stay.
func TestEndToEnd_AdvancedPricing(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "ap-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "ap-guest@test.dev")
	hostTok := host.ID.String()
	guestTok := guest.ID.String()

	// Listing: 100.00/night, 30.00 cleaning, 10% weekly discount, 5% tax.
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Discounted Loft", "type": "apartment", "city": "Porto", "country": "PT",
		"latitude": 41.15, "longitude": -8.61, "priceCents": 10000, "cleaningFeeCents": 3000,
		"currency": "EUR", "maxGuests": 3, "weeklyDiscountPct": 0.10, "taxRatePct": 0.05,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	created := h.decode(rec)
	propID := created["id"].(string)
	if got := created["weeklyDiscountPct"].(float64); got != 0.10 {
		t.Fatalf("weeklyDiscountPct = %v, want 0.10", got)
	}
	uploadPhoto(t, h, hostTok, propID)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil), http.StatusOK, "publish")

	// Book 7 nights → weekly discount applies.
	in := time.Now().UTC().AddDate(0, 0, 10).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 17).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create booking")
	b := h.decode(rec)

	cents := func(field string) float64 {
		return b[field].(map[string]any)["amountCents"].(float64)
	}
	// subtotal 700.00; discount 70.00; base (700-70)+30 = 660.00; service 10% = 66.00;
	// tax 5% = 33.00; total = 759.00.
	if got := cents("subtotal"); got != 70000 {
		t.Fatalf("subtotal = %v, want 70000", got)
	}
	if got := cents("discount"); got != 7000 {
		t.Fatalf("discount = %v, want 7000", got)
	}
	if got := cents("serviceFee"); got != 6600 {
		t.Fatalf("serviceFee = %v, want 6600", got)
	}
	if got := cents("tax"); got != 3300 {
		t.Fatalf("tax = %v, want 3300", got)
	}
	if got := cents("totalPrice"); got != 75900 {
		t.Fatalf("totalPrice = %v, want 75900", got)
	}
}
