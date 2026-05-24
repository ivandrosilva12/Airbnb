package http_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_EarningsCSVExport confirms a booking (crediting the host ledger)
// then downloads the host's earnings CSV statement.
func TestEndToEnd_EarningsCSVExport(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "csv-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "csv-guest@test.dev")
	hostTok := host.ID.String()
	guestTok := guest.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "CSV Loft", "type": "apartment", "city": "Luanda", "country": "AO",
		"latitude": -8.8, "longitude": 13.2, "priceCents": 10000, "currency": "AOA", "maxGuests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil), http.StatusOK, "publish")

	in := time.Now().UTC().AddDate(0, 0, 5).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "create booking")
	bookingID := h.decode(rec)["id"].(string)

	// Host confirms → an earning is credited to the ledger.
	mustStatus(t, h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/confirm", hostTok, nil), http.StatusOK, "confirm")

	// Export the CSV statement.
	rec = h.do(http.MethodGet, "/api/v1/host/earnings/export.csv", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "export csv")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content-type = %q, want text/csv", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "airhost-earnings.csv") {
		t.Fatalf("content-disposition = %q", cd)
	}
	body := rec.Body.String()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 2 {
		t.Fatalf("csv lines = %d, want header + 1 row (body: %q)", len(lines), body)
	}
	if !strings.HasPrefix(lines[0], "date,type,listing,") {
		t.Fatalf("csv header = %q", lines[0])
	}
	if !strings.Contains(lines[1], "earning") || !strings.Contains(lines[1], "CSV Loft") || !strings.Contains(lines[1], "AOA") {
		t.Fatalf("csv row = %q, want earning for CSV Loft in AOA", lines[1])
	}

	// A guest cannot export host earnings.
	if r := h.do(http.MethodGet, "/api/v1/host/earnings/export.csv", guestTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("guest export: status = %d, want 403", r.Code)
	}
}
