package http_test

import (
	"net/http"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_ExperienceBooking_LifecycleAndOverlap walks the booking
// lifecycle for a published experience:
//  1. Host publishes an experience.
//  2. Guest A books it → 201, status=pending.
//  3. Guest A sees it on /experience-bookings/me; the host sees it on
//     /experience-bookings/host.
//  4. Guest B tries the same start time → 409 conflict.
//  5. Host confirms → status=confirmed; guest cannot confirm.
//  6. Guest cancels → status=cancelled.
//
// This pins the S80 contract so later slices (postgres impl, refund
// computation, mobile booking UI) can't silently regress the
// overlap rule or the role gates.
func TestEndToEnd_ExperienceBooking_LifecycleAndOverlap(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "expbk-host@test.dev")
	guestA := h.seedUser(domainuser.RoleGuest, "expbk-a@test.dev")
	guestB := h.seedUser(domainuser.RoleGuest, "expbk-b@test.dev")
	hostTok, aTok, bTok := host.ID.String(), guestA.ID.String(), guestB.ID.String()

	// --- Publish an experience -------------------------------------------------
	rec := h.do(http.MethodPost, "/api/v1/experiences", hostTok, map[string]any{
		"title": "Fresh pasta workshop", "description": "Hands-on, family recipe",
		"category": "cooking",
		"address":  map[string]any{"city": "Rome", "country": "IT", "latitude": 41.9, "longitude": 12.5},
		"durationMinutes": 120, "maxGuests": 6,
		"pricePerGuestCents": 4500, "currency": "EUR", "language": "en",
	})
	mustStatus(t, rec, http.StatusCreated, "create experience")
	expID := h.decode(rec)["id"].(string)
	rec = h.do(http.MethodPost, "/api/v1/experiences/"+expID+"/photos", hostTok, map[string]any{
		"objectKey": "k", "url": "https://example.com/p.jpg",
	})
	mustStatus(t, rec, http.StatusOK, "add photo")
	rec = h.do(http.MethodPost, "/api/v1/experiences/"+expID+"/publish", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "publish experience")

	// --- Guest A books ---------------------------------------------------------
	start := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	rec = h.do(http.MethodPost, "/api/v1/experiences/"+expID+"/bookings", aTok, map[string]any{
		"startAt": start, "guests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "guest A create booking")
	bookingA := h.decode(rec)
	bookingID := bookingA["id"].(string)
	if status := bookingA["status"].(string); status != "pending" {
		t.Errorf("status = %s, want pending", status)
	}
	pricing := bookingA["pricing"].(map[string]any)
	if subtotal := pricing["subtotal"].(map[string]any)["amountCents"].(float64); subtotal != 9000 {
		t.Errorf("subtotal = %v, want 9000", subtotal)
	}

	// --- Lists -----------------------------------------------------------------
	rec = h.do(http.MethodGet, "/api/v1/experience-bookings/me", aTok, nil)
	mustStatus(t, rec, http.StatusOK, "list mine")
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Errorf("guest total = %v, want 1", total)
	}
	rec = h.do(http.MethodGet, "/api/v1/experience-bookings/host", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host list")
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Errorf("host total = %v, want 1", total)
	}

	// --- Overlap conflict ------------------------------------------------------
	rec = h.do(http.MethodPost, "/api/v1/experiences/"+expID+"/bookings", bTok, map[string]any{
		"startAt": start, "guests": 1,
	})
	mustStatus(t, rec, http.StatusConflict, "overlapping booking should 409")

	// --- Confirm — guest forbidden, host allowed ------------------------------
	rec = h.do(http.MethodPost, "/api/v1/experience-bookings/"+bookingID+"/confirm", aTok, nil)
	mustStatus(t, rec, http.StatusForbidden, "guest confirm forbidden")
	rec = h.do(http.MethodPost, "/api/v1/experience-bookings/"+bookingID+"/confirm", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host confirm")
	if status := h.decode(rec)["status"].(string); status != "confirmed" {
		t.Errorf("status = %s, want confirmed", status)
	}

	// --- Cancel — guest allowed -----------------------------------------------
	rec = h.do(http.MethodPost, "/api/v1/experience-bookings/"+bookingID+"/cancel", aTok, nil)
	mustStatus(t, rec, http.StatusOK, "guest cancel")
	if status := h.decode(rec)["status"].(string); status != "cancelled" {
		t.Errorf("status = %s, want cancelled", status)
	}

	// --- After cancel, overlap allows a fresh booking -------------------------
	rec = h.do(http.MethodPost, "/api/v1/experiences/"+expID+"/bookings", bTok, map[string]any{
		"startAt": start, "guests": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "post-cancel re-book at same slot")
}

// TestEndToEnd_ExperienceBooking_DraftRejected proves an unpublished
// listing cannot be booked — the catalogue is the only surface that
// exposes bookable inventory.
func TestEndToEnd_ExperienceBooking_DraftRejected(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "expbk2-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "expbk2-guest@test.dev")
	hostTok, guestTok := host.ID.String(), guest.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/experiences", hostTok, map[string]any{
		"title": "Draft session", "description": "Not yet ready",
		"category": "art",
		"address":  map[string]any{"city": "Lisbon", "country": "PT", "latitude": 38.7, "longitude": -9.1},
		"durationMinutes": 60, "maxGuests": 4,
		"pricePerGuestCents": 2000, "currency": "EUR", "language": "en",
	})
	mustStatus(t, rec, http.StatusCreated, "create draft")
	expID := h.decode(rec)["id"].(string)

	rec = h.do(http.MethodPost, "/api/v1/experiences/"+expID+"/bookings", guestTok, map[string]any{
		"startAt": time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
		"guests":  1,
	})
	mustStatus(t, rec, http.StatusUnprocessableEntity, "draft cannot be booked")
}
