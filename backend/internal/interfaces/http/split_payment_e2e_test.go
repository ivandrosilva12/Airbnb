package http_test

import (
	"net/http"
	"testing"
	"time"

	domainuser "github.com/airhost/backend/internal/domain/user"
)

// seedInstantBookListing creates a published instant-book listing the split
// flow can target. Returns (propID, nightlyPriceCents).
func seedInstantBookListing(t *testing.T, h *harness, hostTok string) (string, int64) {
	t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title":       "Split-friendly Studio",
		"type":        "apartment",
		"city":        "Porto",
		"country":     "PT",
		"latitude":    41.15,
		"longitude":   -8.61,
		"priceCents":  10000, // 100 EUR / night
		"currency":    "EUR",
		"maxGuests":   3,
		"instantBook": true,
	})
	mustStatus(t, rec, http.StatusCreated, "seed instant-book property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "publish instant-book")
	return propID, 10000
}

// bookSplit kicks off a split booking from the organizer with the provided
// share table. Returns (bookingID, totalCents, splitID).
func bookSplit(t *testing.T, h *harness, organizerTok, propID string, in, out string, shares []map[string]any) (string, int64, string) {
	t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/bookings", organizerTok, map[string]any{
		"propertyId":  propID,
		"checkIn":     in,
		"checkOut":    out,
		"guests":      2,
		"splitShares": shares,
	})
	mustStatus(t, rec, http.StatusCreated, "create split booking")
	body := h.decode(rec)
	bookingID := body["id"].(string)
	total := int64(body["totalPrice"].(map[string]any)["amountCents"].(float64))

	// Fetch the split id by booking.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/split", organizerTok, nil)
	mustStatus(t, rec, http.StatusOK, "fetch split for booking")
	splitID := h.decode(rec)["id"].(string)
	return bookingID, total, splitID
}

// TestEndToEnd_SplitHappyPath drives a full split: organizer creates booking
// with two shares, both authorize, booking confirms.
func TestEndToEnd_SplitHappyPath(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "split-host@test.dev")
	org := h.seedUser(domainuser.RoleGuest, "org@test.dev")
	friend := h.seedUser(domainuser.RoleGuest, "friend@test.dev")
	hostTok, orgTok, friendTok := host.ID.String(), org.ID.String(), friend.ID.String()

	propID, _ := seedInstantBookListing(t, h, hostTok)

	in := time.Now().UTC().AddDate(0, 0, 10).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 13).Format("2006-01-02") // 3 nights = 300 EUR base
	// Two shares: organizer 150 EUR + friend 150 EUR. The booking total
	// includes service fee, so we use a placeholder split then validate.
	// First peek at total via a non-split preview... easier: just compute
	// the expected total deterministically.
	// 3 nights × 100 EUR + 10% service fee = 330 EUR = 33000 cents.
	totalCents := int64(33000)
	bookingID, gotTotal, splitID := bookSplit(t, h, orgTok, propID, in, out, []map[string]any{
		{"email": "org@test.dev", "amountCents": 16500},
		{"email": "friend@test.dev", "amountCents": 16500},
	})
	if gotTotal != totalCents {
		t.Fatalf("computed total = %d, expected %d", gotTotal, totalCents)
	}

	// Booking is pending right after creation (instant-book usually confirms
	// immediately, but split bookings wait for every share to be paid).
	rec := h.do(http.MethodGet, "/api/v1/bookings/"+bookingID, orgTok, nil)
	mustStatus(t, rec, http.StatusOK, "fetch booking after split create")
	if status := h.decode(rec)["status"]; status != "pending" {
		t.Fatalf("booking status after split create = %v, want pending", status)
	}

	// Fetch split state.
	rec = h.do(http.MethodGet, "/api/v1/splits/"+splitID, orgTok, nil)
	mustStatus(t, rec, http.StatusOK, "get split as organizer")
	split := h.decode(rec)
	if split["status"] != "pending" {
		t.Fatalf("split status = %v, want pending", split["status"])
	}
	shares := split["shares"].([]any)
	if len(shares) != 2 {
		t.Fatalf("shares = %d, want 2", len(shares))
	}
	orgShareID := shareIDForEmail(t, shares, "org@test.dev")
	friendShareID := shareIDForEmail(t, shares, "friend@test.dev")

	// The friend can see the split too (they're a payer).
	rec = h.do(http.MethodGet, "/api/v1/splits/"+splitID, friendTok, nil)
	mustStatus(t, rec, http.StatusOK, "friend reads split")

	// Friend authorizes first.
	rec = h.do(http.MethodPost, "/api/v1/splits/"+splitID+"/shares/"+friendShareID+"/authorize", friendTok, nil)
	mustStatus(t, rec, http.StatusOK, "friend authorize")
	if h.decode(rec)["status"] != "pending" {
		t.Fatalf("split should still be pending after one share")
	}

	// Booking is still pending too.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID, orgTok, nil)
	if h.decode(rec)["status"] != "pending" {
		t.Fatalf("booking should still be pending after one share")
	}

	// Organizer authorizes their share — now all paid → booking confirms.
	rec = h.do(http.MethodPost, "/api/v1/splits/"+splitID+"/shares/"+orgShareID+"/authorize", orgTok, nil)
	mustStatus(t, rec, http.StatusOK, "organizer authorize")
	body := h.decode(rec)
	if body["status"] != "completed" {
		t.Fatalf("split status = %v, want completed", body["status"])
	}

	// Booking should now be confirmed.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID, orgTok, nil)
	if status := h.decode(rec)["status"]; status != "confirmed" {
		t.Fatalf("booking status after all shares paid = %v, want confirmed", status)
	}
}

// shareIDForEmail picks the share id whose payerEmail matches.
func shareIDForEmail(t *testing.T, shares []any, email string) string {
	t.Helper()
	for _, s := range shares {
		sh := s.(map[string]any)
		if sh["payerEmail"] == email {
			return sh["id"].(string)
		}
	}
	t.Fatalf("no share for email %q", email)
	return ""
}

// TestEndToEnd_SplitRejectsNonInstantBook confirms the split path is gated
// on instant-book listings.
func TestEndToEnd_SplitRejectsNonInstantBook(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "ib-host@test.dev")
	org := h.seedUser(domainuser.RoleGuest, "ib-org@test.dev")
	friend := h.seedUser(domainuser.RoleGuest, "ib-friend@test.dev")
	hostTok, orgTok := host.ID.String(), org.ID.String()
	_ = friend

	// Plain (non-instant) listing.
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Slow Listing", "type": "apartment", "city": "Lisbon", "country": "PT",
		"latitude": 38.7, "longitude": -9.1, "priceCents": 10000, "currency": "EUR", "maxGuests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create non-instant listing")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil)

	in := time.Now().UTC().AddDate(0, 0, 20).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 22).Format("2006-01-02")
	r := h.do(http.MethodPost, "/api/v1/bookings", orgTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 2,
		"splitShares": []map[string]any{
			{"email": "ib-org@test.dev", "amountCents": 11000},
			{"email": "ib-friend@test.dev", "amountCents": 11000},
		},
	})
	if r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("split on non-instant: status = %d, want 422 (body: %s)", r.Code, r.Body.String())
	}
}

// TestEndToEnd_SplitRejectsMismatchedTotal confirms the share sum invariant
// is enforced at the split creation boundary.
func TestEndToEnd_SplitRejectsMismatchedTotal(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "mm-host@test.dev")
	org := h.seedUser(domainuser.RoleGuest, "mm-org@test.dev")
	hostTok, orgTok := host.ID.String(), org.ID.String()

	propID, _ := seedInstantBookListing(t, h, hostTok)

	in := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 33).Format("2006-01-02")
	r := h.do(http.MethodPost, "/api/v1/bookings", orgTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 2,
		"splitShares": []map[string]any{
			{"email": "mm-org@test.dev", "amountCents": 1000},
			{"email": "friend@test.dev", "amountCents": 1000},
		},
	})
	if r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mismatched total: status = %d, want 422 (body: %s)", r.Code, r.Body.String())
	}
}

// TestEndToEnd_SplitAuthorizeWrongPayer confirms a user whose email doesn't
// match a share cannot authorize it.
func TestEndToEnd_SplitAuthorizeWrongPayer(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "wp-host@test.dev")
	org := h.seedUser(domainuser.RoleGuest, "wp-org@test.dev")
	friend := h.seedUser(domainuser.RoleGuest, "wp-friend@test.dev")
	stranger := h.seedUser(domainuser.RoleGuest, "wp-stranger@test.dev")
	hostTok, orgTok, strangerTok := host.ID.String(), org.ID.String(), stranger.ID.String()
	_ = friend

	propID, _ := seedInstantBookListing(t, h, hostTok)
	in := time.Now().UTC().AddDate(0, 0, 40).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 43).Format("2006-01-02") // 3 nights, total 33000
	_, _, splitID := bookSplit(t, h, orgTok, propID, in, out, []map[string]any{
		{"email": "wp-org@test.dev", "amountCents": 16500},
		{"email": "wp-friend@test.dev", "amountCents": 16500},
	})
	rec := h.do(http.MethodGet, "/api/v1/splits/"+splitID, orgTok, nil)
	mustStatus(t, rec, http.StatusOK, "get split")
	shares := h.decode(rec)["shares"].([]any)
	friendShareID := shareIDForEmail(t, shares, "wp-friend@test.dev")

	// Stranger can't even read the split.
	if r := h.do(http.MethodGet, "/api/v1/splits/"+splitID, strangerTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("stranger read split: status = %d, want 403", r.Code)
	}

	// Nor authorize a share.
	if r := h.do(http.MethodPost, "/api/v1/splits/"+splitID+"/shares/"+friendShareID+"/authorize", strangerTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("stranger authorize: status = %d, want 403", r.Code)
	}
}

// TestEndToEnd_SplitOrganizerCancel confirms only the organizer can cancel.
func TestEndToEnd_SplitOrganizerCancel(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "c-host@test.dev")
	org := h.seedUser(domainuser.RoleGuest, "c-org@test.dev")
	friend := h.seedUser(domainuser.RoleGuest, "c-friend@test.dev")
	hostTok, orgTok, friendTok := host.ID.String(), org.ID.String(), friend.ID.String()

	propID, _ := seedInstantBookListing(t, h, hostTok)
	in := time.Now().UTC().AddDate(0, 0, 50).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 53).Format("2006-01-02")
	_, _, splitID := bookSplit(t, h, orgTok, propID, in, out, []map[string]any{
		{"email": "c-org@test.dev", "amountCents": 16500},
		{"email": "c-friend@test.dev", "amountCents": 16500},
	})

	// A payer (non-organizer) can NOT cancel.
	if r := h.do(http.MethodPost, "/api/v1/splits/"+splitID+"/cancel", friendTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("friend cancel: status = %d, want 403", r.Code)
	}

	// Organizer cancels.
	rec := h.do(http.MethodPost, "/api/v1/splits/"+splitID+"/cancel", orgTok, nil)
	mustStatus(t, rec, http.StatusOK, "organizer cancel")
	if h.decode(rec)["status"] != "cancelled" {
		t.Fatalf("split status after cancel = %v, want cancelled", h.decode(rec)["status"])
	}

	// Authorizing a share on a cancelled split is rejected.
	rec = h.do(http.MethodGet, "/api/v1/splits/"+splitID, orgTok, nil)
	shares := h.decode(rec)["shares"].([]any)
	orgShareID := shareIDForEmail(t, shares, "c-org@test.dev")
	if r := h.do(http.MethodPost, "/api/v1/splits/"+splitID+"/shares/"+orgShareID+"/authorize", orgTok, nil); r.Code != http.StatusConflict {
		t.Fatalf("authorize after cancel: status = %d, want 409", r.Code)
	}
}

// TestEndToEnd_SplitListMine confirms the per-user "my splits" view returns
// splits I organize AND splits I'm a payer in.
func TestEndToEnd_SplitListMine(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "lm-host@test.dev")
	org := h.seedUser(domainuser.RoleGuest, "lm-org@test.dev")
	friend := h.seedUser(domainuser.RoleGuest, "lm-friend@test.dev")
	hostTok, orgTok, friendTok := host.ID.String(), org.ID.String(), friend.ID.String()

	propID, _ := seedInstantBookListing(t, h, hostTok)
	in := time.Now().UTC().AddDate(0, 0, 60).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 63).Format("2006-01-02")
	_, _, splitID := bookSplit(t, h, orgTok, propID, in, out, []map[string]any{
		{"email": "lm-org@test.dev", "amountCents": 16500},
		{"email": "lm-friend@test.dev", "amountCents": 16500},
	})

	// Organizer sees it.
	rec := h.do(http.MethodGet, "/api/v1/me/splits", orgTok, nil)
	mustStatus(t, rec, http.StatusOK, "organizer my splits")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != splitID {
		t.Fatalf("organizer splits = %v, want one with id %s", items, splitID)
	}

	// Friend sees it too (payer).
	rec = h.do(http.MethodGet, "/api/v1/me/splits", friendTok, nil)
	mustStatus(t, rec, http.StatusOK, "friend my splits")
	items = h.decode(rec)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != splitID {
		t.Fatalf("friend splits = %v, want one with id %s", items, splitID)
	}
}
