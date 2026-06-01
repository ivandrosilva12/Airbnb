package http_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// seedStayWithArrival builds a published listing with the supplied arrival
// info plus a confirmed booking whose check-in lands `daysUntilCheckIn` from
// `now`. The booking length is 2 nights. Returns the booking so callers can
// hit /api/v1/bookings/<id>/arrival.
func (h *harness) seedStayWithArrival(guestID, hostID uuid.UUID, daysUntilCheckIn int, info property.ArrivalInfo) *booking.Booking {
	h.t.Helper()
	ctx := context.Background()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Lisboa", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	p, err := property.NewProperty(hostID, "Apto com self check-in", "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		h.t.Fatalf("new property: %v", err)
	}
	if err := p.SetArrivalInfo(info); err != nil {
		h.t.Fatalf("arrival info: %v", err)
	}
	if err := h.propertyRepo.Create(ctx, p); err != nil {
		h.t.Fatalf("store property: %v", err)
	}
	start := time.Now().UTC().AddDate(0, 0, daysUntilCheckIn)
	dr, err := booking.NewDateRange(start, start.AddDate(0, 0, 2))
	if err != nil {
		h.t.Fatalf("date range: %v", err)
	}
	b, err := booking.NewBooking(p.ID, guestID, dr, 1, price, cleaning, 0, booking.Discounts{})
	if err != nil {
		h.t.Fatalf("new booking: %v", err)
	}
	_ = b.Confirm()
	if err := h.bookingRepo.Create(ctx, b); err != nil {
		h.t.Fatalf("store booking: %v", err)
	}
	return b
}

func arrivalSample() property.ArrivalInfo {
	return property.ArrivalInfo{
		CheckInMethod: property.CheckInMethodSelfLockbox,
		Instructions:  "Lockbox #4 by the entrance. Code 4242.",
		WifiSSID:      "AirHost-Net",
		WifiPassword:  "tagus-river-42",
	}
}

// TestEndToEnd_ArrivalRevealedInsideWindow covers the headline S16 behaviour:
// the guest sees the credentials once the booking is ≤ 48h from check-in,
// and not before.
func TestEndToEnd_ArrivalRevealedInsideWindow(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "arr-host-in@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "arr-guest-in@test.dev")
	guestTok := guest.ID.String()

	// Booking starts tomorrow — well inside the 48h reveal window.
	b := h.seedStayWithArrival(guest.ID, host.ID, 1, arrivalSample())

	rec := h.do(http.MethodGet, "/api/v1/bookings/"+b.ID.String()+"/arrival", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "arrival inside window")
	v := h.decode(rec)
	if v["checkInMethod"] != string(property.CheckInMethodSelfLockbox) {
		t.Fatalf("method = %v", v["checkInMethod"])
	}
	if v["wifiSsid"] != "AirHost-Net" || v["wifiPassword"] != "tagus-river-42" {
		t.Fatalf("wifi creds wrong: %v", v)
	}
}

// TestEndToEnd_ArrivalHiddenBeforeWindow checks the negative case: a booking
// 5 days away is too far out — the credentials must stay hidden (403).
func TestEndToEnd_ArrivalHiddenBeforeWindow(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "arr-host-early@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "arr-guest-early@test.dev")
	guestTok := guest.ID.String()

	b := h.seedStayWithArrival(guest.ID, host.ID, 5, arrivalSample())

	rec := h.do(http.MethodGet, "/api/v1/bookings/"+b.ID.String()+"/arrival", guestTok, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (outside window)", rec.Code)
	}
}

// TestEndToEnd_ArrivalRejectsThirdParty makes sure even inside the window a
// non-owner cannot read someone else's arrival info.
func TestEndToEnd_ArrivalRejectsThirdParty(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "arr-host-third@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "arr-guest-third@test.dev")
	stranger := h.seedUser(domainuser.RoleGuest, "arr-stranger@test.dev")

	b := h.seedStayWithArrival(guest.ID, host.ID, 1, arrivalSample())

	rec := h.do(http.MethodGet, "/api/v1/bookings/"+b.ID.String()+"/arrival", stranger.ID.String(), nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("third-party read status = %d, want 403", rec.Code)
	}
}

// TestEndToEnd_PropertyHidesArrivalFromPublic verifies the listing detail
// endpoint never embeds arrival info for non-owners (anonymous or
// authenticated guest), even when configured.
func TestEndToEnd_PropertyHidesArrivalFromPublic(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "arr-host-pub@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "arr-guest-pub@test.dev")

	b := h.seedStayWithArrival(guest.ID, host.ID, 1, arrivalSample())

	// Anonymous read of the property detail.
	rec := h.do(http.MethodGet, "/api/v1/properties/"+b.PropertyID.String(), "", nil)
	mustStatus(t, rec, http.StatusOK, "anon property read")
	v := h.decode(rec)
	if v["arrival"] != nil {
		t.Fatalf("anonymous property view leaked arrival: %v", v["arrival"])
	}

	// Guest-authenticated read of the same property — also no arrival block.
	rec = h.do(http.MethodGet, "/api/v1/properties/"+b.PropertyID.String(), guest.ID.String(), nil)
	mustStatus(t, rec, http.StatusOK, "guest property read")
	v = h.decode(rec)
	if v["arrival"] != nil {
		t.Fatalf("guest property view leaked arrival: %v", v["arrival"])
	}

	// Host read on the dedicated authenticated endpoint — arrival block IS present.
	rec = h.do(http.MethodGet, "/api/v1/host/properties/"+b.PropertyID.String(), host.ID.String(), nil)
	mustStatus(t, rec, http.StatusOK, "host property read")
	v = h.decode(rec)
	arr, _ := v["arrival"].(map[string]any)
	if arr == nil {
		t.Fatalf("host property view missing arrival block: %v", v)
	}
	if arr["wifiPassword"] != "tagus-river-42" {
		t.Fatalf("host view wifi password = %v", arr["wifiPassword"])
	}

	// A non-host attempting the host endpoint is 403'd.
	rec = h.do(http.MethodGet, "/api/v1/host/properties/"+b.PropertyID.String(), guest.ID.String(), nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-host on /host/properties/<id>: status = %d, want 403", rec.Code)
	}
}

// TestEndToEnd_ArrivalHiddenAfterCancellation makes sure a cancelled booking
// can never see the credentials, even if the reveal window is otherwise open.
func TestEndToEnd_ArrivalHiddenAfterCancellation(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "arr-host-cxl@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "arr-guest-cxl@test.dev")
	guestTok := guest.ID.String()

	b := h.seedStayWithArrival(guest.ID, host.ID, 1, arrivalSample())
	// Cancel the booking directly via the repo so we don't have to fight
	// the booking lifecycle for a clean state.
	_ = b.Cancel()
	if err := h.bookingRepo.Update(context.Background(), b); err != nil {
		t.Fatalf("update cancelled booking: %v", err)
	}

	rec := h.do(http.MethodGet, "/api/v1/bookings/"+b.ID.String()+"/arrival", guestTok, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cancelled booking arrival status = %d, want 403", rec.Code)
	}
}
