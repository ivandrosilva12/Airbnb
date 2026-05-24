package http_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// TestEndToEnd_RealtimeNotifications drives the SSE endpoint over a real HTTP
// connection: a host opens the stream, a guest books their listing, and the
// host must receive a live "notification" hint.
func TestEndToEnd_RealtimeNotifications(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "rt-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "rt-guest@test.dev")
	hostTok := host.ID.String()
	guestTok := guest.ID.String()

	// Host creates, photographs and publishes a listing.
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "RT Loft", "type": "apartment", "city": "Lisbon", "country": "PT",
		"latitude": 38.7, "longitude": -9.1, "priceCents": 10000, "currency": "EUR", "maxGuests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil), http.StatusOK, "publish")

	// A real server so we can hold a streaming connection open.
	srv := httptest.NewServer(h.router)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/realtime?access_token="+hostTok, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d, want 200", resp.StatusCode)
	}

	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	// Wait for the initial ": connected" comment so the subscription is live
	// before we trigger an event.
	waitForLine(t, lines, func(s string) bool { return strings.Contains(s, "connected") }, "connected comment")

	// Guest books → BookingRequested → host receives a realtime "notification".
	in := time.Now().UTC().AddDate(0, 0, 5).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 8).Format("2006-01-02")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	}), http.StatusCreated, "create booking")

	waitForLine(t, lines, func(s string) bool {
		return strings.HasPrefix(s, "data:") && strings.Contains(s, "notification")
	}, "notification event")

	// An unauthenticated stream request is rejected.
	anon, err := http.Get(srv.URL + "/api/v1/realtime")
	if err != nil {
		t.Fatalf("anonymous SSE: %v", err)
	}
	defer anon.Body.Close()
	if anon.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous SSE status = %d, want 401", anon.StatusCode)
	}
}

func waitForLine(t *testing.T, lines <-chan string, match func(string) bool, what string) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed before %s", what)
			}
			if match(line) {
				return
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}
