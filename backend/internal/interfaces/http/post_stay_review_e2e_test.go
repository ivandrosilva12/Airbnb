package http_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// seedCompletedStay stores a published property and a completed booking for the
// guest directly in the repositories, since completion requires a past
// check-out the booking API would reject at create time.
func (h *harness) seedCompletedStay(guestID, hostID uuid.UUID, title string) *booking.Booking {
	h.t.Helper()
	ctx := context.Background()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Porto", Country: "PT", Latitude: 41.1, Longitude: -8.6}
	p, err := property.NewProperty(hostID, title, "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		h.t.Fatalf("new property: %v", err)
	}
	if err := h.propertyRepo.Create(ctx, p); err != nil {
		h.t.Fatalf("store property: %v", err)
	}
	start := time.Now().UTC().AddDate(0, 0, 3)
	dr, err := booking.NewDateRange(start, start.AddDate(0, 0, 2))
	if err != nil {
		h.t.Fatalf("date range: %v", err)
	}
	b, err := booking.NewBooking(p.ID, guestID, dr, 1, price, cleaning, 0, booking.Discounts{})
	if err != nil {
		h.t.Fatalf("new booking: %v", err)
	}
	_ = b.Confirm()
	_ = b.Complete()
	if err := h.bookingRepo.Create(ctx, b); err != nil {
		h.t.Fatalf("store booking: %v", err)
	}
	// Seed a matching captured Payment so the post-stay flow can refund through
	// it. We drive the FakeGateway directly so refunds during the test resolve
	// the gateway-issued reference correctly. Tests that only care about the
	// booking lifecycle ignore the payment.
	pay := payment.New(b.ID, guestID, price)
	ref, err := h.paymentGateway.Authorize(ctx, price, b.ID.String())
	if err != nil {
		h.t.Fatalf("authorize seed payment: %v", err)
	}
	if err := pay.Authorize(ref); err != nil {
		h.t.Fatalf("apply authorize: %v", err)
	}
	if err := h.paymentGateway.Capture(ctx, ref); err != nil {
		h.t.Fatalf("capture seed payment at gateway: %v", err)
	}
	if err := pay.Capture(); err != nil {
		h.t.Fatalf("apply capture: %v", err)
	}
	if err := h.paymentRepo.Create(ctx, pay); err != nil {
		h.t.Fatalf("store seed payment: %v", err)
	}
	return b
}

// TestEndToEnd_PostStayReview covers the post-stay review prompt: a completed
// stay shows up in the guest's pending list, and once reviewed it disappears.
func TestEndToEnd_PostStayReview(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "ps-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "ps-guest@test.dev")
	guestTok := guest.ID.String()

	b := h.seedCompletedStay(guest.ID, host.ID, "Seaside Flat")

	// The completed stay is pending review.
	rec := h.do(http.MethodGet, "/api/v1/me/reviews/pending", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "pending reviews")
	items := h.decode(rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("pending = %d, want 1", len(items))
	}
	first := items[0].(map[string]any)
	if first["bookingId"].(string) != b.ID.String() {
		t.Fatalf("bookingId = %v, want %v", first["bookingId"], b.ID)
	}
	if first["propertyTitle"].(string) != "Seaside Flat" {
		t.Fatalf("propertyTitle = %v", first["propertyTitle"])
	}

	// The guest reviews the stay.
	rec = h.do(http.MethodPost, "/api/v1/reviews", guestTok, map[string]any{
		"bookingId": b.ID.String(), "rating": 5, "comment": "Wonderful",
	})
	mustStatus(t, rec, http.StatusCreated, "create review")

	// It is no longer pending.
	rec = h.do(http.MethodGet, "/api/v1/me/reviews/pending", guestTok, nil)
	if items := h.decode(rec)["items"].([]any); len(items) != 0 {
		t.Fatalf("pending after review = %d, want 0", len(items))
	}
}
