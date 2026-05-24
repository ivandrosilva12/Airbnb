package payoutapp_test

import (
	"context"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	payoutapp "github.com/airhost/backend/internal/application/payout"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
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

	svc := payoutapp.NewService(payouts, bookings, props)
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

func TestEventHandler_DebitsRefundOnCancel(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	payouts := memory.NewPayoutRepository()
	hostID := uuid.New()
	b := seed(t, props, bookings, hostID)

	svc := payoutapp.NewService(payouts, bookings, props)
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

func TestEventHandler_NoRefundWithoutPriorEarning(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	payouts := memory.NewPayoutRepository()
	hostID := uuid.New()
	b := seed(t, props, bookings, hostID)

	svc := payoutapp.NewService(payouts, bookings, props)
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
