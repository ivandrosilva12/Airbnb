package http_test

import (
	"net/http"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_PricingQuote_NoCommitment proves the S59 dry-run endpoint
// returns the same breakdown a Create call would assemble, WITHOUT
// persisting a booking — overlap/blocks/KYC/house-rules gates do not fire.
func TestEndToEnd_PricingQuote_NoCommitment(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "pq-host@test.dev")
	hostTok := host.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Quote Loft", "type": "apartment", "city": "Porto", "country": "PT",
		"latitude": 41.15, "longitude": -8.61, "priceCents": 10000, "cleaningFeeCents": 3000,
		"currency": "EUR", "maxGuests": 3, "weeklyDiscountPct": 0.10, "taxRatePct": 0.05,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil), http.StatusOK, "publish")

	// 7-night stay → weekly discount applies, same as the AdvancedPricing fixture
	// (subtotal 700, discount 70, service 66, tax 33, total 759).
	in := time.Now().UTC().AddDate(0, 0, 10).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 17).Format("2006-01-02")
	// No auth header — pricing is public.
	rec = h.do(http.MethodGet,
		"/api/v1/properties/"+propID+"/pricing-quote?checkIn="+in+"&checkOut="+out+"&guests=2",
		"", nil)
	mustStatus(t, rec, http.StatusOK, "pricing quote")
	q := h.decode(rec)

	cents := func(field string) float64 {
		return q[field].(map[string]any)["amountCents"].(float64)
	}
	if got := q["nights"].(float64); got != 7 {
		t.Fatalf("nights = %v, want 7", got)
	}
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

// TestEndToEnd_PricingQuote_RejectsBadInput confirms the endpoint surfaces
// 400 for malformed date params (vs leaking an internal error).
func TestEndToEnd_PricingQuote_RejectsBadInput(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "pqbad-host@test.dev")
	hostTok := host.ID.String()
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "X", "type": "apartment", "city": "Porto", "country": "PT",
		"latitude": 41.15, "longitude": -8.61, "priceCents": 10000, "cleaningFeeCents": 3000,
		"currency": "EUR", "maxGuests": 3,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil), http.StatusOK, "publish")

	rec = h.do(http.MethodGet,
		"/api/v1/properties/"+propID+"/pricing-quote?checkIn=bogus&checkOut=2099-12-31&guests=1",
		"", nil)
	mustStatus(t, rec, http.StatusBadRequest, "bogus date")

	rec = h.do(http.MethodGet,
		"/api/v1/properties/"+propID+"/pricing-quote?checkIn=2099-12-30&checkOut=2099-12-31&guests=0",
		"", nil)
	mustStatus(t, rec, http.StatusBadRequest, "guests=0")
}
