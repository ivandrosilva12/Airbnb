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
	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway())

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

	// Booking cancelled -> refunded.
	dispatcher.Publish(ctx, event.BookingCancelled{BookingID: bookingID, GuestID: guestID})
	p, _ = svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusRefunded {
		t.Fatalf("status after cancel = %s, want refunded", p.Status)
	}
}
