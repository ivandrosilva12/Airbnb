package paymentapp_test

import (
	"context"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	paymentapp "github.com/airhost/backend/internal/application/payment"
	"github.com/airhost/backend/internal/application/port"
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

func TestReconcileGatewayEvent_CaptureRefundIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPaymentRepository()
	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway(), memory.NewBookingRepository(), memory.NewPropertyRepository())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	bookingID := uuid.New()
	guestID := uuid.New()
	dispatcher.Publish(ctx, event.BookingRequested{BookingID: bookingID, GuestID: guestID, TotalCents: 30000, Currency: "EUR"})
	p, _ := svc.GetForBooking(ctx, guestID, bookingID)
	ref := p.GatewayRef // untagged fake ref; provider unknown to the verifier

	// A "captured" webhook moves authorized -> captured.
	changed, err := svc.ReconcileGatewayEvent(ctx, port.GatewayEvent{Provider: "fake", Reference: ref, Type: port.GatewayCaptured})
	if err != nil {
		t.Fatalf("reconcile capture: %v", err)
	}
	if !changed {
		t.Fatal("expected the capture event to change state")
	}
	p, _ = svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusCaptured {
		t.Fatalf("status = %s, want captured", p.Status)
	}

	// Re-delivering the same event is a no-op (idempotent).
	changed, err = svc.ReconcileGatewayEvent(ctx, port.GatewayEvent{Provider: "fake", Reference: ref, Type: port.GatewayCaptured})
	if err != nil {
		t.Fatalf("reconcile capture (replay): %v", err)
	}
	if changed {
		t.Fatal("replayed capture should be a no-op")
	}

	// A partial "refunded" webhook records the refund.
	changed, err = svc.ReconcileGatewayEvent(ctx, port.GatewayEvent{Provider: "fake", Reference: ref, Type: port.GatewayRefunded, AmountCents: 12000})
	if err != nil {
		t.Fatalf("reconcile refund: %v", err)
	}
	if !changed {
		t.Fatal("expected the refund event to change state")
	}
	p, _ = svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusRefunded || p.RefundedCents != 12000 {
		t.Fatalf("after refund = status %s refunded %d, want refunded 12000", p.Status, p.RefundedCents)
	}

	// An event for an unknown reference is a safe no-op (not an error).
	changed, err = svc.ReconcileGatewayEvent(ctx, port.GatewayEvent{Provider: "fake", Reference: "nope", Type: port.GatewayCaptured})
	if err != nil || changed {
		t.Fatalf("unknown ref: changed=%v err=%v, want false/nil", changed, err)
	}
}

func TestReconcileGatewayEvent_FailedAuthorization(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPaymentRepository()
	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway(), memory.NewBookingRepository(), memory.NewPropertyRepository())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	bookingID := uuid.New()
	guestID := uuid.New()
	dispatcher.Publish(ctx, event.BookingRequested{BookingID: bookingID, GuestID: guestID, TotalCents: 5000, Currency: "EUR"})
	p, _ := svc.GetForBooking(ctx, guestID, bookingID)

	changed, err := svc.ReconcileGatewayEvent(ctx, port.GatewayEvent{Provider: "fake", Reference: p.GatewayRef, Type: port.GatewayFailed, FailureReason: "bank declined"})
	if err != nil || !changed {
		t.Fatalf("reconcile failed: changed=%v err=%v", changed, err)
	}
	p, _ = svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusFailed || p.FailureReason != "bank declined" {
		t.Fatalf("after fail = status %s reason %q, want failed/bank declined", p.Status, p.FailureReason)
	}
}
