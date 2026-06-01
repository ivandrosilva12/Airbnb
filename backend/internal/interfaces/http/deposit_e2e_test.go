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

// seedCompletedStayWithDeposit is like seedCompletedStay but also configures a
// security deposit on the listing and seeds a captured rental Payment plus an
// authorized DepositHold whose gateway ref the FakeGateway will accept on
// refund/capture. It returns the booking and the deposit hold for assertions.
func (h *harness) seedCompletedStayWithDeposit(guestID, hostID uuid.UUID, title string, depositCents int64) (*booking.Booking, *payment.DepositHold) {
	h.t.Helper()
	ctx := context.Background()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	deposit, _ := shared.NewMoney(depositCents, "EUR")
	addr := property.Address{City: "Lisboa", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	p, err := property.NewProperty(hostID, title, "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		h.t.Fatalf("new property: %v", err)
	}
	// Configure the security deposit. min/max/guests stay at defaults; the
	// extra-guest fee must share the currency.
	extra, _ := shared.NewMoney(0, "EUR")
	if err := p.SetStayRules(1, 0, p.MaxGuests, extra, deposit); err != nil {
		h.t.Fatalf("stay rules: %v", err)
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

	// Rental payment captured at the gateway.
	pay := payment.New(b.ID, guestID, price)
	ref, err := h.paymentGateway.Authorize(ctx, price, b.ID.String())
	if err != nil {
		h.t.Fatalf("authorize rental: %v", err)
	}
	_ = pay.Authorize(ref)
	if err := h.paymentGateway.Capture(ctx, ref); err != nil {
		h.t.Fatalf("capture rental: %v", err)
	}
	_ = pay.Capture()
	if err := h.paymentRepo.Create(ctx, pay); err != nil {
		h.t.Fatalf("store rental payment: %v", err)
	}

	// Deposit hold authorized at the gateway.
	d, err := payment.NewDepositHold(b.ID, guestID, deposit)
	if err != nil {
		h.t.Fatalf("new deposit: %v", err)
	}
	depRef, err := h.paymentGateway.Authorize(ctx, deposit, "deposit:"+b.ID.String())
	if err != nil {
		h.t.Fatalf("authorize deposit: %v", err)
	}
	_ = d.Authorize(depRef)
	if err := h.depositRepo.Create(ctx, d); err != nil {
		h.t.Fatalf("store deposit: %v", err)
	}
	return b, d
}

// TestEndToEnd_DepositVisibleToGuest covers the read path of the deposit:
// once the booking is confirmed and the hold is placed, the guest can fetch
// /bookings/:id/deposit and see the amount, status and remaining capacity.
func TestEndToEnd_DepositVisibleToGuest(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "dep-host-view@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "dep-guest-view@test.dev")
	guestTok := guest.ID.String()

	b, _ := h.seedCompletedStayWithDeposit(guest.ID, host.ID, "Loft com depósito", 20000)

	rec := h.do(http.MethodGet, "/api/v1/bookings/"+b.ID.String()+"/deposit", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "guest deposit view")
	v := h.decode(rec)
	if v["status"] != "authorized" {
		t.Fatalf("status = %v, want authorized", v["status"])
	}
	if rem, _ := v["remainingCents"].(float64); int64(rem) != 20000 {
		t.Fatalf("remaining = %v, want 20000", v["remainingCents"])
	}

	// A third party cannot read someone else's deposit.
	other := h.seedUser(domainuser.RoleGuest, "dep-other-view@test.dev")
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+b.ID.String()+"/deposit", other.ID.String(), nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("other-user deposit read should be denied, got %d", rec.Code)
	}
}

// TestEndToEnd_DamageClaimDrawsFromDepositFirst is the headline S15 behaviour:
// a damage decision under the deposit cap is paid in full from the hold and
// leaves no audit-only damage_claim row on the rental payment.
func TestEndToEnd_DamageClaimDrawsFromDepositFirst(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "dep-host-claim@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "dep-guest-claim@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "dep-admin-claim@test.dev")
	guestTok, adminTok := guest.ID.String(), admin.ID.String()

	b, _ := h.seedCompletedStayWithDeposit(guest.ID, host.ID, "Casa com depósito", 20000)
	bookingID := b.ID.String()

	// Host opens a damage dispute. Damage disputes need an amount > 0.
	rec := h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/disputes", host.ID.String(), map[string]any{
		"kind": "damage", "reason": "Broken kitchen tile.", "requestedAmountCents": 15000, "currency": "EUR",
	})
	mustStatus(t, rec, http.StatusCreated, "open damage dispute")
	disputeID := h.decode(rec)["id"].(string)

	// Admin awards the host 15000 cents in damage — well under the 20000 cap.
	rec = h.do(http.MethodPost, "/api/v1/admin/disputes/"+disputeID+"/resolve", adminTok, map[string]any{
		"resolution":        "Damage confirmed; awarding 15000 cents from the deposit.",
		"damageAmountCents": 15000,
	})
	mustStatus(t, rec, http.StatusOK, "admin resolve with damage")

	// The deposit now shows 15000 captured and 5000 remaining.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/deposit", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "deposit after capture")
	dep := h.decode(rec)
	if cap, _ := dep["capturedCents"].(float64); int64(cap) != 15000 {
		t.Fatalf("deposit captured = %v, want 15000", dep["capturedCents"])
	}
	if rem, _ := dep["remainingCents"].(float64); int64(rem) != 5000 {
		t.Fatalf("deposit remaining = %v, want 5000", dep["remainingCents"])
	}
	if dep["status"] != "partially_captured" {
		t.Fatalf("deposit status = %v, want partially_captured", dep["status"])
	}
	adjs, _ := dep["adjustments"].([]any)
	if len(adjs) != 1 {
		t.Fatalf("deposit adjustments = %d, want 1", len(adjs))
	}
	first := adjs[0].(map[string]any)
	if first["kind"] != "deposit_capture" || first["refKind"] != "dispute" || first["refId"] != disputeID {
		t.Fatalf("adjustment payload unexpected: %v", first)
	}

	// The rental payment should NOT carry any damage_claim row (deposit covered
	// the whole amount).
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/payment", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "rental payment after damage")
	pay := h.decode(rec)
	if dc, _ := pay["damageClaimCents"].(float64); int64(dc) != 0 {
		t.Fatalf("rental damageClaimCents = %v, want 0 (deposit covered)", pay["damageClaimCents"])
	}
}

// TestEndToEnd_DamageOverflowFallsBackToAuditRow covers the case where the
// awarded damage exceeds the deposit cap: the deposit is captured in full and
// the leftover lands as an audit-only damage_claim on the rental payment.
func TestEndToEnd_DamageOverflowFallsBackToAuditRow(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "dep-host-over@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "dep-guest-over@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "dep-admin-over@test.dev")
	guestTok, adminTok := guest.ID.String(), admin.ID.String()

	b, _ := h.seedCompletedStayWithDeposit(guest.ID, host.ID, "Quinta com depósito", 10000)
	bookingID := b.ID.String()

	rec := h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/disputes", host.ID.String(), map[string]any{
		"kind": "damage", "reason": "Sofá manchado e janela partida.", "requestedAmountCents": 15000, "currency": "EUR",
	})
	mustStatus(t, rec, http.StatusCreated, "open big damage dispute")
	disputeID := h.decode(rec)["id"].(string)

	rec = h.do(http.MethodPost, "/api/v1/admin/disputes/"+disputeID+"/resolve", adminTok, map[string]any{
		"resolution":        "Awarding 15000 cents — deposit caps at 10000, balance recorded.",
		"damageAmountCents": 15000,
	})
	mustStatus(t, rec, http.StatusOK, "admin resolve overflow")

	// Deposit fully captured.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/deposit", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "deposit after full capture")
	dep := h.decode(rec)
	if dep["status"] != "captured" {
		t.Fatalf("deposit status = %v, want captured", dep["status"])
	}
	if rem, _ := dep["remainingCents"].(float64); int64(rem) != 0 {
		t.Fatalf("deposit remaining = %v, want 0", rem)
	}

	// Rental payment carries the 5000-cent overflow as a damage_claim audit row.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/payment", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "rental payment after overflow")
	pay := h.decode(rec)
	if dc, _ := pay["damageClaimCents"].(float64); int64(dc) != 5000 {
		t.Fatalf("rental damageClaimCents = %v, want 5000 overflow", pay["damageClaimCents"])
	}
}
