package http_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/payment"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// TestEndToEnd_PaymentWebhook drives the async reconciliation path: a booking
// authorizes a payment, then a signed GPay Angola webhook captures it. It also
// checks signature rejection and unknown-provider handling.
func TestEndToEnd_PaymentWebhook(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "wh-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "wh-guest@test.dev")
	hostTok := host.ID.String()
	guestTok := guest.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Webhook Loft", "type": "apartment", "city": "Luanda", "country": "AO",
		"latitude": -8.8, "longitude": 13.2, "priceCents": 10000, "currency": "AOA", "maxGuests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)

	// A listing needs a photo and must be published before it accepts bookings.
	uploadPhoto(t, h, hostTok, propID)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil),
		http.StatusOK, "publish property")

	in := time.Now().UTC().AddDate(0, 0, 5).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "create booking")
	bookingID := uuid.MustParse(h.decode(rec)["id"].(string))

	// The booking authorized a payment; read its gateway reference from the repo.
	p, err := h.paymentRepo.FindByBookingID(context.Background(), bookingID)
	if err != nil {
		t.Fatalf("find payment: %v", err)
	}
	if string(p.Status) != "authorized" {
		t.Fatalf("payment status = %s, want authorized", p.Status)
	}

	body := []byte(`{"event":"captured","id":"` + p.GatewayRef + `"}`)

	// A correctly signed webhook captures the payment.
	resp := h.doRaw(http.MethodPost, "/api/v1/webhooks/payments/gpayangola", body, map[string]string{
		"Content-Type": "application/json",
		"X-Signature":  sign(webhookSecret, body),
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, want 200 (body: %s)", resp.Code, resp.Body.String())
	}
	p, _ = h.paymentRepo.FindByBookingID(context.Background(), bookingID)
	if string(p.Status) != "captured" {
		t.Fatalf("payment status after webhook = %s, want captured", p.Status)
	}

	// Re-delivering the identical webhook is de-duplicated at storage level.
	resp = h.doRaw(http.MethodPost, "/api/v1/webhooks/payments/gpayangola", body, map[string]string{
		"Content-Type": "application/json",
		"X-Signature":  sign(webhookSecret, body),
	})
	mustStatus(t, resp, http.StatusOK, "replayed webhook")
	if status := h.decode(resp)["status"].(string); status != "duplicate" {
		t.Fatalf("replayed webhook status = %q, want duplicate", status)
	}

	// A bad signature is rejected.
	if r := h.doRaw(http.MethodPost, "/api/v1/webhooks/payments/gpayangola", body, map[string]string{
		"Content-Type": "application/json",
		"X-Signature":  "deadbeef",
	}); r.Code != http.StatusBadRequest {
		t.Fatalf("bad signature: status = %d, want 400", r.Code)
	}

	// An unconfigured provider is a 404.
	if r := h.doRaw(http.MethodPost, "/api/v1/webhooks/payments/stripe", body, map[string]string{
		"Content-Type": "application/json",
		"X-Signature":  sign(webhookSecret, body),
	}); r.Code != http.StatusNotFound {
		t.Fatalf("unconfigured provider: status = %d, want 404", r.Code)
	}

	// Retention cleanup is admin-only.
	admin := h.seedUser(domainuser.RoleAdmin, "wh-admin@test.dev")
	if r := h.do(http.MethodPost, "/api/v1/admin/webhooks/events/cleanup", guestTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("guest cleanup: status = %d, want 403", r.Code)
	}
	// olderThanDays=0 prunes everything recorded before now, including the
	// event captured above.
	rec = h.do(http.MethodPost, "/api/v1/admin/webhooks/events/cleanup?olderThanDays=0", admin.ID.String(), nil)
	mustStatus(t, rec, http.StatusOK, "cleanup")
	if deleted := h.decode(rec)["deleted"].(float64); deleted < 1 {
		t.Fatalf("cleanup deleted = %v, want >= 1", deleted)
	}
}

// rewindToPending mutates a payment row back to pending status so a webhook
// test can simulate an async-auth scenario: the initial Authorize returned a
// pending reference (no error, no transition), and the gateway will signal
// completion via webhook later. The fake gateway always succeeds
// synchronously, so we have to forge the in-between state here.
func rewindToPending(t *testing.T, h *harness, bookingID uuid.UUID) *payment.Payment {
	t.Helper()
	p, err := h.paymentRepo.FindByBookingID(context.Background(), bookingID)
	if err != nil {
		t.Fatalf("find payment: %v", err)
	}
	p.Status = payment.StatusPending
	if err := h.paymentRepo.Update(context.Background(), p); err != nil {
		t.Fatalf("rewind payment: %v", err)
	}
	return p
}

// TestEndToEnd_PaymentWebhook_AsyncAuthorizeAutoConfirms drives the
// WF-GAP-010 hook end-to-end: a signed GPay Angola "authorized" webhook
// arrives for a payment whose initial Authorize returned a pending
// reference, and an instant-book booking that has been stuck in pending
// auto-confirms. A duplicate delivery is idempotent — the booking stays
// confirmed without a second BookingConfirmed transition.
func TestEndToEnd_PaymentWebhook_AsyncAuthorizeAutoConfirms(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "wh-async-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "wh-async-guest@test.dev")
	hostTok := host.ID.String()
	guestTok := guest.ID.String()

	// 1. Host creates and publishes an instant-book listing.
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Async Loft", "type": "apartment", "city": "Luanda", "country": "AO",
		"latitude": -8.8, "longitude": 13.2, "priceCents": 10000, "currency": "AOA", "maxGuests": 2,
		"instantBook": true,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil),
		http.StatusOK, "publish property")

	in := time.Now().UTC().AddDate(0, 0, 5).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "create booking")
	bookingID := uuid.MustParse(h.decode(rec)["id"].(string))

	// 2. Rewind the booking to pending + payment to pending — the state the
	// system would be in if the gateway returned a pending ref (instead of
	// the fake gateway's immediate success). This is the WF-GAP-010 gap:
	// without the new hook the booking would be stuck here.
	b, err := h.bookingRepo.FindByID(context.Background(), bookingID)
	if err != nil {
		t.Fatalf("find booking: %v", err)
	}
	b.Status = "pending"
	if err := h.bookingRepo.Update(context.Background(), b); err != nil {
		t.Fatalf("rewind booking: %v", err)
	}
	p := rewindToPending(t, h, bookingID)

	// 3. The async "authorized" webhook arrives.
	body := []byte(`{"event":"authorized","id":"` + p.GatewayRef + `","eventId":"gp_auth_1"}`)
	resp := h.doRaw(http.MethodPost, "/api/v1/webhooks/payments/gpayangola", body, map[string]string{
		"Content-Type": "application/json",
		"X-Signature":  sign(webhookSecret, body),
	})
	mustStatus(t, resp, http.StatusOK, "async authorized webhook")

	// 4. The booking auto-confirmed. The payment ends up captured: the
	// reconciler moved it to authorized, PaymentAuthorized fired, the
	// booking subscriber confirmed the reservation, BookingConfirmed fired,
	// and the payment subscriber captured the hold — the full async
	// happy-path cascade we'd see in production once 3DS clears.
	p, err = h.paymentRepo.FindByBookingID(context.Background(), bookingID)
	if err != nil {
		t.Fatalf("find payment: %v", err)
	}
	if string(p.Status) != "captured" {
		t.Fatalf("payment status after webhook = %s, want captured (auth -> confirm -> capture cascade)", p.Status)
	}
	b, err = h.bookingRepo.FindByID(context.Background(), bookingID)
	if err != nil {
		t.Fatalf("find booking: %v", err)
	}
	if string(b.Status) != "confirmed" {
		t.Fatalf("booking status after webhook = %s, want confirmed (auto-confirm via PaymentAuthorized)", b.Status)
	}

	// 5. A duplicate delivery is de-duplicated at storage level — the
	// booking stays confirmed and no second BookingConfirmed transition
	// fires (the payment row is already authorized, so the reconciler
	// itself short-circuits before publishing again).
	resp = h.doRaw(http.MethodPost, "/api/v1/webhooks/payments/gpayangola", body, map[string]string{
		"Content-Type": "application/json",
		"X-Signature":  sign(webhookSecret, body),
	})
	mustStatus(t, resp, http.StatusOK, "duplicate authorized webhook")
	if status := h.decode(resp)["status"].(string); status != "duplicate" {
		t.Fatalf("replayed webhook status = %q, want duplicate", status)
	}
	b, _ = h.bookingRepo.FindByID(context.Background(), bookingID)
	if string(b.Status) != "confirmed" {
		t.Fatalf("booking status after duplicate = %s, want still confirmed", b.Status)
	}
}

// TestEndToEnd_PaymentWebhook_AsyncAuthorizeLeavesRequestToBookPending
// covers the inverse of the previous test: a request-to-book listing
// (instant-book OFF) whose payment finished its async authorization must
// NOT be auto-confirmed. The webhook still moves the payment to
// authorized, but the booking stays pending until the host approves it
// through the normal Confirm flow.
func TestEndToEnd_PaymentWebhook_AsyncAuthorizeLeavesRequestToBookPending(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "wh-req-host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "wh-req-guest@test.dev")
	hostTok := host.ID.String()
	guestTok := guest.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Request Loft", "type": "apartment", "city": "Luanda", "country": "AO",
		"latitude": -8.8, "longitude": 13.2, "priceCents": 10000, "currency": "AOA", "maxGuests": 2,
		// instantBook omitted/false: this is a request-to-book listing.
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)
	uploadPhoto(t, h, hostTok, propID)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil),
		http.StatusOK, "publish property")

	in := time.Now().UTC().AddDate(0, 0, 5).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	})
	mustStatus(t, rec, http.StatusCreated, "create booking")
	bookingID := uuid.MustParse(h.decode(rec)["id"].(string))

	// The booking is already pending (request-to-book). Rewind only the
	// payment to pending to simulate the async-auth scenario.
	p := rewindToPending(t, h, bookingID)

	body := []byte(`{"event":"authorized","id":"` + p.GatewayRef + `","eventId":"gp_auth_req_1"}`)
	resp := h.doRaw(http.MethodPost, "/api/v1/webhooks/payments/gpayangola", body, map[string]string{
		"Content-Type": "application/json",
		"X-Signature":  sign(webhookSecret, body),
	})
	mustStatus(t, resp, http.StatusOK, "async authorized webhook (request-to-book)")

	p, err := h.paymentRepo.FindByBookingID(context.Background(), bookingID)
	if err != nil {
		t.Fatalf("find payment: %v", err)
	}
	if string(p.Status) != "authorized" {
		t.Fatalf("payment status after webhook = %s, want authorized", p.Status)
	}
	b, err := h.bookingRepo.FindByID(context.Background(), bookingID)
	if err != nil {
		t.Fatalf("find booking: %v", err)
	}
	if string(b.Status) != "pending" {
		t.Fatalf("booking status after webhook = %s, want still pending (host approval required)", b.Status)
	}
}
