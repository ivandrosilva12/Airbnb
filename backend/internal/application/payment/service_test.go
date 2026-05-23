package paymentapp_test

import (
	"context"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	paymentapp "github.com/airhost/backend/internal/application/payment"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/shared"
	infrapayment "github.com/airhost/backend/internal/infrastructure/payment"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func TestPaymentLifecycleFromEvents(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPaymentRepository()
	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway(), memory.NewBookingRepository(), memory.NewPropertyRepository())

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	bookingID := uuid.New()
	guestID := uuid.New()

	// Booking requested -> payment authorized for the total.
	dispatcher.Publish(ctx, event.BookingRequested{
		BookingID: bookingID, GuestID: guestID, TotalCents: 42900, Currency: "EUR",
	})
	p, err := svc.GetForBooking(ctx, guestID, bookingID)
	if err != nil {
		t.Fatalf("get payment: %v", err)
	}
	if p.Status != payment.StatusAuthorized {
		t.Fatalf("status = %s, want authorized", p.Status)
	}
	if p.Amount.AmountCents() != 42900 {
		t.Fatalf("amount = %d, want 42900", p.Amount.AmountCents())
	}
	if p.GatewayRef == "" {
		t.Fatal("expected a gateway reference after authorization")
	}

	// A non-owner cannot read the payment.
	if _, err := svc.GetForBooking(ctx, uuid.New(), bookingID); err != shared.ErrForbidden {
		t.Fatalf("cross-user read err = %v, want ErrForbidden", err)
	}

	// Booking confirmed -> captured.
	dispatcher.Publish(ctx, event.BookingConfirmed{BookingID: bookingID, GuestID: guestID})
	p, _ = svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusCaptured {
		t.Fatalf("status after confirm = %s, want captured", p.Status)
	}

	// Booking cancelled with a 50% policy refund -> partial refund of the
	// captured charge.
	dispatcher.Publish(ctx, event.BookingCancelled{BookingID: bookingID, GuestID: guestID, RefundFraction: 0.5})
	p, _ = svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusRefunded {
		t.Fatalf("status after cancel = %s, want refunded", p.Status)
	}
	if p.RefundedCents != 21450 { // 50% of 42900
		t.Fatalf("refunded = %d, want 21450", p.RefundedCents)
	}
}

func TestCancel_ReleasesAuthorizedHoldInFull(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPaymentRepository()
	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway(), memory.NewBookingRepository(), memory.NewPropertyRepository())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	bookingID := uuid.New()
	guestID := uuid.New()
	dispatcher.Publish(ctx, event.BookingRequested{BookingID: bookingID, GuestID: guestID, TotalCents: 10000, Currency: "EUR"})

	// Not yet captured (still authorized): cancelling releases the whole hold,
	// regardless of the policy fraction.
	dispatcher.Publish(ctx, event.BookingCancelled{BookingID: bookingID, GuestID: guestID, RefundFraction: 0})
	p, _ := svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusRefunded || p.RefundedCents != 10000 {
		t.Fatalf("authorized hold release = status %s refunded %d, want refunded 10000", p.Status, p.RefundedCents)
	}
}
