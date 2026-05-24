package http_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
	"time"

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
}
