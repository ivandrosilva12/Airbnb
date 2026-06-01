package http_test

import (
	"net/http"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// seedHighValueListing creates and publishes a listing priced so a 3-night
// stay comfortably exceeds the 1000-EUR step-up threshold the harness wires
// in (500 EUR / night × 3 nights = 1500 EUR base alone). Returns the listing
// id.
func seedHighValueListing(t *testing.T, h *harness, hostTok string) string {
	t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title":      "Premium Penthouse",
		"type":       "apartment",
		"city":       "Lisbon",
		"country":    "PT",
		"latitude":   38.72,
		"longitude":  -9.14,
		"priceCents": 50000, // 500 EUR per night
		"currency":   "EUR",
		"maxGuests":  2,
	})
	mustStatus(t, rec, http.StatusCreated, "seed high-value property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "publish")
	return propID
}

// verifyGuest walks the guest through the KYC submit + admin approve flow so
// IsVerified returns true on subsequent calls.
func verifyGuest(t *testing.T, h *harness, guestTok, adminTok string) {
	t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/me/verification", guestTok, map[string]any{
		"documentType": "passport",
		"documentRef":  "P1234567",
		"legalName":    "Stepup Guest",
	})
	mustStatus(t, rec, http.StatusCreated, "submit verification")
	verID := h.decode(rec)["id"].(string)
	rec = h.do(http.MethodPost, "/api/v1/admin/verifications/"+verID+"/approve", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "approve verification")
}

// TestEndToEnd_KYCStepUpBlocksHighValueBooking confirms that an unverified
// guest booking above the configured threshold gets a 422 with the typed
// kyc_step_up_required code and structured details.
func TestEndToEnd_KYCStepUpBlocksHighValueBooking(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "stepup-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "stepup-guest@test.dev")
	hostTok, guestTok := host.ID.String(), guest.ID.String()

	propID := seedHighValueListing(t, h, hostTok)

	in := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 33).Format("2006-01-02") // 3 nights × 500 EUR

	rec := h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 2,
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("high-value booking unverified: status = %d, want 422 (body: %s)", rec.Code, rec.Body.String())
	}
	body := h.decode(rec)
	if body["code"] != "kyc_step_up_required" {
		t.Fatalf("code = %v, want kyc_step_up_required (body: %s)", body["code"], rec.Body.String())
	}
	details, ok := body["details"].(map[string]any)
	if !ok {
		t.Fatalf("details missing or wrong shape: %v", body["details"])
	}
	if details["currency"] != "EUR" {
		t.Fatalf("details.currency = %v, want EUR", details["currency"])
	}
	if details["thresholdCents"].(float64) != 100000 {
		t.Fatalf("details.thresholdCents = %v, want 100000", details["thresholdCents"])
	}
}

// TestEndToEnd_KYCStepUpVerifiedGuestPasses confirms that once the guest is
// verified, the same high-value booking goes through.
func TestEndToEnd_KYCStepUpVerifiedGuestPasses(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "stepup-host2@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "stepup-guest2@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "stepup-admin@test.dev")
	hostTok, guestTok, adminTok := host.ID.String(), guest.ID.String(), admin.ID.String()

	propID := seedHighValueListing(t, h, hostTok)
	verifyGuest(t, h, guestTok, adminTok)

	in := time.Now().UTC().AddDate(0, 0, 40).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 43).Format("2006-01-02") // 3 nights × 500 EUR

	rec := h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "verified guest high-value booking")
}

// TestEndToEnd_KYCStepUpBelowThresholdSkipsCheck confirms that a booking
// total below the threshold doesn't require verification — the gate only
// engages above the configured limit.
func TestEndToEnd_KYCStepUpBelowThresholdSkipsCheck(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "lowval-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "lowval-guest@test.dev")
	hostTok, guestTok := host.ID.String(), guest.ID.String()

	// 100 EUR / night × 2 nights = 200 EUR all-in, well below 1000 EUR.
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title":      "Cosy Studio",
		"type":       "apartment",
		"city":       "Porto",
		"country":    "PT",
		"latitude":   41.15,
		"longitude":  -8.61,
		"priceCents": 10000,
		"currency":   "EUR",
		"maxGuests":  2,
	})
	mustStatus(t, rec, http.StatusCreated, "seed cheap property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "publish cheap")

	in := time.Now().UTC().AddDate(0, 0, 10).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 12).Format("2006-01-02") // 2 nights

	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "low-value booking proceeds without KYC")
}

// TestEndToEnd_KYCStepUpUnknownCurrencyHasNoGate confirms a currency without
// a configured threshold simply has no step-up — the harness wires only EUR,
// so a USD booking of any size should proceed without verification.
func TestEndToEnd_KYCStepUpUnknownCurrencyHasNoGate(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "usd-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "usd-guest@test.dev")
	hostTok, guestTok := host.ID.String(), guest.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title":      "USD Listing",
		"type":       "apartment",
		"city":       "Anywhere",
		"country":    "US",
		"latitude":   40.7,
		"longitude":  -74.0,
		"priceCents": 100000, // 1000 USD / night
		"currency":   "USD",
		"maxGuests":  2,
	})
	mustStatus(t, rec, http.StatusCreated, "seed USD property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "publish USD")

	in := time.Now().UTC().AddDate(0, 0, 50).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 55).Format("2006-01-02") // 5 nights — well above the EUR threshold but in USD

	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "USD booking proceeds (no USD threshold configured)")
}
