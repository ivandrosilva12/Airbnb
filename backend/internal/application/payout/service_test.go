package payoutapp_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	payoutapp "github.com/airhost/backend/internal/application/payout"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	infrapayment "github.com/airhost/backend/internal/infrastructure/payment"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// seed builds a published property owned by hostID and a confirmed-eligible
// booking on it, persisting both, and returns the booking. The pricing is a
// 3-night stay at 100.00 + 30.00 cleaning with a 10% service fee, so:
// subtotal 300.00, cleaning 30.00, service fee 33.00, total 363.00, net 330.00.
func seed(t *testing.T, props *memory.PropertyRepository, bookings *memory.BookingRepository, hostID uuid.UUID) *booking.Booking {
	t.Helper()
	price, _ := shared.NewMoney(10000, "EUR")
	cleaning, _ := shared.NewMoney(3000, "EUR")
	prop, err := property.NewProperty(hostID, "Loft", "", property.TypeApartment,
		property.Address{City: "Lisbon", Country: "PT"}, price, cleaning, 4, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	if err := props.Create(context.Background(), prop); err != nil {
		t.Fatalf("create property: %v", err)
	}

	in := time.Now().UTC().AddDate(0, 0, 5)
	out := time.Now().UTC().AddDate(0, 0, 8)
	dates, err := booking.NewDateRange(in, out)
	if err != nil {
		t.Fatalf("date range: %v", err)
	}
	b, err := booking.NewBooking(prop.ID, uuid.New(), dates, 2, price, cleaning, 0.10, booking.Discounts{})
	if err != nil {
		t.Fatalf("new booking: %v", err)
	}
	if err := bookings.Create(context.Background(), b); err != nil {
		t.Fatalf("create booking: %v", err)
	}
	return b
}

func TestEventHandler_CreditsHostOnConfirm(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	payouts := memory.NewPayoutRepository()
	hostID := uuid.New()
	b := seed(t, props, bookings, hostID)

	svc := payoutapp.NewService(payouts, bookings, props, memory.NewUserRepository(), infrapayment.NewFakeDisburser(), infrapayment.NewFakeConnectGateway())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	dispatcher.Publish(ctx, event.BookingConfirmed{BookingID: b.ID, PropertyID: b.PropertyID, GuestID: b.GuestID})

	balances, err := svc.Summary(ctx, hostID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(balances) != 1 || balances[0].NetCents() != 33000 {
		t.Fatalf("host balance = %+v, want net 33000 EUR", balances)
	}

	page, err := svc.ListEntries(ctx, hostID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Kind != payout.KindEarning {
		t.Fatalf("entries = %+v, want one earning", page.Items)
	}
}

func TestEventHandler_DuplicateConfirmCreditsOnce(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	payouts := memory.NewPayoutRepository()
	hostID := uuid.New()
	b := seed(t, props, bookings, hostID)

	svc := payoutapp.NewService(payouts, bookings, props, memory.NewUserRepository(), infrapayment.NewFakeDisburser(), infrapayment.NewFakeConnectGateway())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	// At-least-once delivery: the same BookingConfirmed arriving twice must credit
	// the host only once (idempotency guard), not double the balance.
	ev := event.BookingConfirmed{BookingID: b.ID, PropertyID: b.PropertyID, GuestID: b.GuestID}
	dispatcher.Publish(ctx, ev)
	dispatcher.Publish(ctx, ev)

	page, err := svc.ListEntries(ctx, hostID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("entries after duplicate confirm = %d, want 1", len(page.Items))
	}
	balances, _ := svc.Summary(ctx, hostID)
	if len(balances) != 1 || balances[0].NetCents() != 33000 {
		t.Fatalf("host balance = %+v, want net 33000 EUR (credited once)", balances)
	}
}

func TestEventHandler_DebitsRefundOnCancel(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	payouts := memory.NewPayoutRepository()
	hostID := uuid.New()
	b := seed(t, props, bookings, hostID)

	svc := payoutapp.NewService(payouts, bookings, props, memory.NewUserRepository(), infrapayment.NewFakeDisburser(), infrapayment.NewFakeConnectGateway())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	// Earn, then cancel with a half refund: net 330.00 → refund 165.00 → balance 165.00.
	dispatcher.Publish(ctx, event.BookingConfirmed{BookingID: b.ID, PropertyID: b.PropertyID, GuestID: b.GuestID})
	dispatcher.Publish(ctx, event.BookingCancelled{
		BookingID: b.ID, PropertyID: b.PropertyID, HostID: hostID, GuestID: b.GuestID,
		CancelledBy: b.GuestID, RefundFraction: 0.5,
	})

	balances, err := svc.Summary(ctx, hostID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(balances) != 1 || balances[0].NetCents() != 16500 {
		t.Fatalf("host balance after refund = %+v, want net 16500 EUR", balances)
	}
}

func TestExportEntries_FlattensLedgerWithTitlesAndSign(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	payouts := memory.NewPayoutRepository()
	hostID := uuid.New()
	b := seed(t, props, bookings, hostID)

	svc := payoutapp.NewService(payouts, bookings, props, memory.NewUserRepository(), infrapayment.NewFakeDisburser(), infrapayment.NewFakeConnectGateway())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	// One earning (+330.00) and one half refund (−165.00).
	dispatcher.Publish(ctx, event.BookingConfirmed{BookingID: b.ID, PropertyID: b.PropertyID, GuestID: b.GuestID})
	dispatcher.Publish(ctx, event.BookingCancelled{
		BookingID: b.ID, PropertyID: b.PropertyID, HostID: hostID, GuestID: b.GuestID,
		CancelledBy: b.GuestID, RefundFraction: 0.5,
	})

	rows, err := svc.ExportEntries(ctx, hostID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	var earning, refund *payoutapp.ExportRow
	for i := range rows {
		switch rows[i].Kind {
		case "earning":
			earning = &rows[i]
		case "refund":
			refund = &rows[i]
		}
	}
	if earning == nil || refund == nil {
		t.Fatalf("expected one earning and one refund, got %+v", rows)
	}
	if earning.SignedCents != 33000 {
		t.Fatalf("earning signed = %d, want 33000", earning.SignedCents)
	}
	if refund.SignedCents != -16500 {
		t.Fatalf("refund signed = %d, want -16500", refund.SignedCents)
	}
	if earning.PropertyTitle != "Loft" || earning.Currency != "EUR" {
		t.Fatalf("enrichment wrong: %+v", earning)
	}
}

func TestRequestDisbursement_PaysAvailableThenExhausts(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	payouts := memory.NewPayoutRepository()
	users := memory.NewUserRepository()
	hostID := uuid.New()
	b := seed(t, props, bookings, hostID)

	// The host must have an onboarded, payouts-enabled connected account.
	host := &user.User{
		ID: hostID, KeycloakSub: "sub-" + hostID.String(), Email: "host@ex.dev", FullName: "Host",
		Role: user.RoleHost, IsActive: true, PayoutAccountID: "acct_test", PayoutsEnabled: true,
	}
	if err := users.Create(ctx, host); err != nil {
		t.Fatalf("seed host: %v", err)
	}

	svc := payoutapp.NewService(payouts, bookings, props, users, infrapayment.NewFakeDisburser(), infrapayment.NewFakeConnectGateway())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())
	dispatcher.Publish(ctx, event.BookingConfirmed{BookingID: b.ID, PropertyID: b.PropertyID, GuestID: b.GuestID})

	// Net earning is 330.00 EUR; pay it all out.
	d, err := svc.RequestDisbursement(ctx, hostID, "EUR")
	if err != nil {
		t.Fatalf("request disbursement: %v", err)
	}
	if d.Status != payout.DisbursementPaid || d.AmountCents != 33000 || d.GatewayRef == "" {
		t.Fatalf("disbursement = %+v, want paid 33000 EUR with a reference", d)
	}

	// The available balance is now exhausted (the paid payout is committed).
	avail, err := svc.AvailableBalances(ctx, hostID)
	if err != nil {
		t.Fatalf("available: %v", err)
	}
	for _, a := range avail {
		if a.Currency == "EUR" && a.AvailableCents != 0 {
			t.Fatalf("available EUR = %d, want 0 after payout", a.AvailableCents)
		}
	}

	// A second request finds nothing to pay out.
	if _, err := svc.RequestDisbursement(ctx, hostID, "EUR"); err == nil {
		t.Fatal("expected second payout to fail with no available funds")
	}
}

func TestStartOnboarding_EnablesPayoutsViaFakeRail(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	hostID := uuid.New()
	host := &user.User{
		ID: hostID, KeycloakSub: "sub-" + hostID.String(), Email: "h@ex.dev", FullName: "H",
		Role: user.RoleHost, IsActive: true,
	}
	if err := users.Create(ctx, host); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	svc := payoutapp.NewService(
		memory.NewPayoutRepository(), memory.NewBookingRepository(), memory.NewPropertyRepository(),
		users, infrapayment.NewFakeDisburser(), infrapayment.NewFakeConnectGateway(),
	)

	url, err := svc.StartOnboarding(ctx, hostID, "https://app/refresh", "https://app/return")
	if err != nil {
		t.Fatalf("onboard: %v", err)
	}
	if url == "" {
		t.Fatal("expected an onboarding url")
	}
	st, err := svc.AccountStatus(ctx, hostID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.HasAccount || !st.Enabled {
		t.Fatalf("status = %+v, want onboarded + enabled (fake rail)", st)
	}
}

func TestRequestDisbursement_ConcurrentRequestsPayOnce(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	payouts := memory.NewPayoutRepository()
	users := memory.NewUserRepository()
	hostID := uuid.New()
	b := seed(t, props, bookings, hostID) // net 330.00 EUR earning on confirm

	host := &user.User{
		ID: hostID, KeycloakSub: "sub-" + hostID.String(), Email: "h@ex.dev", FullName: "H",
		Role: user.RoleHost, IsActive: true, PayoutAccountID: "acct_test", PayoutsEnabled: true,
	}
	if err := users.Create(ctx, host); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	svc := payoutapp.NewService(payouts, bookings, props, users, infrapayment.NewFakeDisburser(), infrapayment.NewFakeConnectGateway())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())
	dispatcher.Publish(ctx, event.BookingConfirmed{BookingID: b.ID, PropertyID: b.PropertyID, GuestID: b.GuestID})

	// Fire many concurrent payout requests for the same host; the per-host lock
	// must let exactly one reserve the (single) available balance.
	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ok int
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := svc.RequestDisbursement(ctx, hostID, "EUR"); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if ok != 1 {
		t.Fatalf("successful payouts = %d, want exactly 1 (no double-pay)", ok)
	}
	page, err := svc.ListDisbursements(ctx, hostID, shared.NewPage(50, 0))
	if err != nil {
		t.Fatalf("list disbursements: %v", err)
	}
	var paid int64
	var paidCount int
	for _, d := range page.Items {
		if d.Status == payout.DisbursementPaid {
			paid += d.AmountCents
			paidCount++
		}
	}
	if paidCount != 1 || paid != 33000 {
		t.Fatalf("paid disbursements = %d totalling %d, want 1 totalling 33000", paidCount, paid)
	}
}

func TestSyncAccountFromWebhook_TogglesPayouts(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	hostID := uuid.New()
	host := &user.User{
		ID: hostID, KeycloakSub: "sub-" + hostID.String(), Email: "h@ex.dev", FullName: "H",
		Role: user.RoleHost, IsActive: true, PayoutAccountID: "acct_42", PayoutsEnabled: false,
	}
	if err := users.Create(ctx, host); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	svc := payoutapp.NewService(
		memory.NewPayoutRepository(), memory.NewBookingRepository(), memory.NewPropertyRepository(),
		users, infrapayment.NewFakeDisburser(), infrapayment.NewFakeConnectGateway(),
	)

	// An unknown account is a harmless no-op.
	if changed, err := svc.SyncAccountFromWebhook(ctx, "acct_unknown", true); err != nil || changed {
		t.Fatalf("unknown account: changed=%v err=%v, want no-op", changed, err)
	}

	// The host's account flips to enabled.
	changed, err := svc.SyncAccountFromWebhook(ctx, "acct_42", true)
	if err != nil || !changed {
		t.Fatalf("sync: changed=%v err=%v, want changed", changed, err)
	}
	st, err := svc.AccountStatus(ctx, hostID)
	if err != nil || !st.Enabled {
		t.Fatalf("status = %+v err=%v, want enabled", st, err)
	}

	// Re-delivery of the same state is idempotent (no change).
	if changed, err := svc.SyncAccountFromWebhook(ctx, "acct_42", true); err != nil || changed {
		t.Fatalf("redelivery: changed=%v err=%v, want no-op", changed, err)
	}
}

func TestRequestDisbursement_BlockedWithoutOnboarding(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	payouts := memory.NewPayoutRepository()
	users := memory.NewUserRepository()
	hostID := uuid.New()
	b := seed(t, props, bookings, hostID)
	// Host exists but has not onboarded a payout account.
	host := &user.User{
		ID: hostID, KeycloakSub: "sub-" + hostID.String(), Email: "h@ex.dev", FullName: "H",
		Role: user.RoleHost, IsActive: true,
	}
	if err := users.Create(ctx, host); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	svc := payoutapp.NewService(payouts, bookings, props, users, infrapayment.NewFakeDisburser(), infrapayment.NewFakeConnectGateway())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())
	dispatcher.Publish(ctx, event.BookingConfirmed{BookingID: b.ID, PropertyID: b.PropertyID, GuestID: b.GuestID})

	if _, err := svc.RequestDisbursement(ctx, hostID, "EUR"); err == nil {
		t.Fatal("expected disbursement to be blocked before payout onboarding")
	}
}

func TestEventHandler_NoRefundWithoutPriorEarning(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	payouts := memory.NewPayoutRepository()
	hostID := uuid.New()
	b := seed(t, props, bookings, hostID)

	svc := payoutapp.NewService(payouts, bookings, props, memory.NewUserRepository(), infrapayment.NewFakeDisburser(), infrapayment.NewFakeConnectGateway())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	// A booking cancelled before confirmation never credited the host, so there
	// is nothing to reverse and no ledger entry is created.
	dispatcher.Publish(ctx, event.BookingCancelled{
		BookingID: b.ID, PropertyID: b.PropertyID, HostID: hostID, GuestID: b.GuestID,
		CancelledBy: b.GuestID, RefundFraction: 1.0,
	})

	page, err := svc.ListEntries(ctx, hostID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("entries = %+v, want none", page.Items)
	}
}
