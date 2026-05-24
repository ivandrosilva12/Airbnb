package http_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_CalendarSync covers the iCal export feed (bookings + blocks) and
// importing an external feed as host blocks.
func TestEndToEnd_CalendarSync(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "ical-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "ical-guest@test.dev")
	hostTok := host.ID.String()
	guestTok := guest.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "iCal Loft", "type": "apartment", "city": "Sintra", "country": "PT",
		"latitude": 38.8, "longitude": -9.39, "priceCents": 9000, "currency": "EUR", "maxGuests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil), http.StatusOK, "publish")

	// A guest books, creating a busy range.
	in := time.Now().UTC().AddDate(0, 0, 5)
	out := time.Now().UTC().AddDate(0, 0, 8)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in.Format("2006-01-02"), "checkOut": out.Format("2006-01-02"), "guests": 1,
	}), http.StatusCreated, "create booking")

	// Export: the public .ics feed advertises the booked range.
	rec = h.do(http.MethodGet, "/api/v1/properties/"+propID+"/calendar.ics", "", nil)
	mustStatus(t, rec, http.StatusOK, "calendar export")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/calendar") {
		t.Fatalf("calendar content-type = %q, want text/calendar", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "BEGIN:VEVENT") || !strings.Contains(body, "DTSTART;VALUE=DATE:"+in.Format("20060102")) {
		t.Fatalf("export missing the booked event:\n%s", body)
	}

	// Import: an external feed blocks two future nights.
	impFrom := time.Now().UTC().AddDate(0, 0, 30)
	impTo := time.Now().UTC().AddDate(0, 0, 32)
	feed := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:ext-1\r\n" +
		fmt.Sprintf("DTSTART;VALUE=DATE:%s\r\nDTEND;VALUE=DATE:%s\r\n", impFrom.Format("20060102"), impTo.Format("20060102")) +
		"SUMMARY:Booked elsewhere\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/calendar/import", hostTok, map[string]any{"ical": feed})
	mustStatus(t, rec, http.StatusOK, "calendar import")
	if imported := h.decode(rec)["imported"].(float64); imported != 1 {
		t.Fatalf("imported = %v, want 1", imported)
	}

	// The imported range now shows as blocked, and a booking over it is rejected.
	rec = h.do(http.MethodGet, "/api/v1/properties/"+propID+"/availability?from="+impFrom.Format("2006-01-02")+"&to="+impTo.Format("2006-01-02"), "", nil)
	booked := h.decode(rec)["booked"].([]any)
	if len(booked) != 1 || booked[0].(map[string]any)["status"] != "blocked" {
		t.Fatalf("expected one blocked range after import, got %v", booked)
	}
	if r := h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": impFrom.Format("2006-01-02"), "checkOut": impTo.Format("2006-01-02"), "guests": 1,
	}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("booking over imported block: status = %d, want 422", r.Code)
	}

	// Re-importing the same feed is idempotent (nothing new created).
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/calendar/import", hostTok, map[string]any{"ical": feed})
	mustStatus(t, rec, http.StatusOK, "calendar re-import")
	if imported := h.decode(rec)["imported"].(float64); imported != 0 {
		t.Fatalf("re-import imported = %v, want 0", imported)
	}

	// A guest cannot import to someone else's listing (host-only).
	if r := h.do(http.MethodPost, "/api/v1/properties/"+propID+"/calendar/import", guestTok, map[string]any{"ical": feed}); r.Code != http.StatusForbidden {
		t.Fatalf("guest import: status = %d, want 403", r.Code)
	}
}
