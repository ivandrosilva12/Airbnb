package paymentapp_test

import (
	"context"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	paymentapp "github.com/airhost/backend/internal/application/payment"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/experiencebooking"
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

func TestModified_AdjustsAuthorizedHold(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPaymentRepository()
	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway(), memory.NewBookingRepository(), memory.NewPropertyRepository())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	bookingID := uuid.New()
	guestID := uuid.New()

	// Initial hold for the original total.
	dispatcher.Publish(ctx, event.BookingRequested{BookingID: bookingID, GuestID: guestID, TotalCents: 30000, Currency: "EUR"})

	// The guest extends the stay; the booking re-prices and the hold is adjusted.
	dispatcher.Publish(ctx, event.BookingModified{BookingID: bookingID, GuestID: guestID, TotalCents: 50000, Currency: "EUR"})
	p, err := svc.GetForBooking(ctx, guestID, bookingID)
	if err != nil {
		t.Fatalf("get payment: %v", err)
	}
	if p.Status != payment.StatusAuthorized {
		t.Fatalf("status after modify = %s, want authorized", p.Status)
	}
	if p.Amount.AmountCents() != 50000 {
		t.Fatalf("hold after modify = %d, want 50000", p.Amount.AmountCents())
	}

	// A redelivery of the same modification is a no-op (idempotent).
	dispatcher.Publish(ctx, event.BookingModified{BookingID: bookingID, GuestID: guestID, TotalCents: 50000, Currency: "EUR"})
	p, _ = svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusAuthorized || p.Amount.AmountCents() != 50000 {
		t.Fatalf("after redelivery = status %s amount %d, want authorized 50000", p.Status, p.Amount.AmountCents())
	}

	// Capturing now charges the adjusted amount.
	dispatcher.Publish(ctx, event.BookingConfirmed{BookingID: bookingID, GuestID: guestID})
	p, _ = svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusCaptured {
		t.Fatalf("status after confirm = %s, want captured", p.Status)
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

// capturingPublisher records every event the service publishes — used by
// TestReconcileGatewayEvent_AsyncAuthorized to assert that exactly one
// PaymentAuthorized was fanned out after the webhook moved the local
// payment from pending to authorized.
type capturingPublisher struct{ events []event.Event }

func (p *capturingPublisher) Publish(_ context.Context, ev event.Event) {
	p.events = append(p.events, ev)
}

// TestReconcileGatewayEvent_AsyncAuthorized covers the WF-GAP-010 webhook
// path: the gateway authorized asynchronously and reports it via a webhook,
// which moves a still-pending local payment to authorized and emits
// PaymentAuthorized so the booking context can auto-confirm. A duplicate
// delivery is a no-op (no second event, no state change).
func TestReconcileGatewayEvent_AsyncAuthorized(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPaymentRepository()
	pub := &capturingPublisher{}
	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway(), memory.NewBookingRepository(), memory.NewPropertyRepository()).
		WithPublisher(pub)

	// Seed a pending payment with a gateway ref — the state we'd be in if
	// the initial Authorize returned "pending" (no error, no transition).
	bookingID := uuid.New()
	guestID := uuid.New()
	amount, _ := shared.NewMoney(20000, "EUR")
	p := payment.New(bookingID, guestID, amount)
	p.GatewayRef = "pi_async_007"
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Webhook arrives: async authorization completed.
	changed, err := svc.ReconcileGatewayEvent(ctx, port.GatewayEvent{
		Provider: "fake", Reference: "pi_async_007", Type: port.GatewayAuthorized,
	})
	if err != nil {
		t.Fatalf("reconcile authorized: %v", err)
	}
	if !changed {
		t.Fatal("expected the authorize event to change state")
	}
	stored, _ := svc.GetForBooking(ctx, guestID, bookingID)
	if stored.Status != payment.StatusAuthorized {
		t.Fatalf("status = %s, want authorized", stored.Status)
	}
	if len(pub.events) != 1 {
		t.Fatalf("published %d events, want 1 (PaymentAuthorized)", len(pub.events))
	}
	authEv, ok := pub.events[0].(event.PaymentAuthorized)
	if !ok {
		t.Fatalf("event 0 = %T, want PaymentAuthorized", pub.events[0])
	}
	if authEv.BookingID != bookingID || authEv.GuestID != guestID || authEv.GatewayRef != "pi_async_007" {
		t.Fatalf("event payload = %+v, want booking=%s guest=%s ref=pi_async_007", authEv, bookingID, guestID)
	}

	// Duplicate delivery: storage-level dedupe sits in the handler, but the
	// reconciler is also idempotent — a second call finds the payment
	// already authorized and short-circuits, with no extra event.
	changed, err = svc.ReconcileGatewayEvent(ctx, port.GatewayEvent{
		Provider: "fake", Reference: "pi_async_007", Type: port.GatewayAuthorized,
	})
	if err != nil {
		t.Fatalf("reconcile authorized (replay): %v", err)
	}
	if changed {
		t.Fatal("replayed authorize should be a no-op")
	}
	if len(pub.events) != 1 {
		t.Fatalf("published %d events after replay, want still 1", len(pub.events))
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

// TestEventHandler_ExperienceBookingLifecycle drives the payment subscriber
// off the three experiencebooking lifecycle events (Created → Confirmed →
// Cancelled) and asserts authorize / capture / refund were called with the
// expected amount and booking id (S87, WF-GAP-015). The previous slice left
// the payment subscriber blind to experience bookings, so a guest could
// reserve a session and never be charged.
func TestEventHandler_ExperienceBookingLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPaymentRepository()
	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway(), memory.NewBookingRepository(), memory.NewPropertyRepository())

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	bookingID := uuid.New()
	guestID := uuid.New()
	hostID := uuid.New()
	experienceID := uuid.New()
	const totalCents = 12500
	const currency = "EUR"

	// Created → authorized.
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCreated{
		BookingID:       bookingID,
		ExperienceID:    experienceID,
		ExperienceTitle: "Pasta workshop",
		HostID:          hostID,
		GuestID:         guestID,
		TotalCents:      totalCents,
		Currency:        currency,
		OccurredAt:      time.Now().UTC(),
	})

	p, err := svc.GetForBooking(ctx, guestID, bookingID)
	if err != nil {
		t.Fatalf("get payment after Created: %v", err)
	}
	if p.Status != payment.StatusAuthorized {
		t.Fatalf("status after Created = %s, want authorized", p.Status)
	}
	if p.Amount.AmountCents() != totalCents {
		t.Fatalf("amount after Created = %d, want %d", p.Amount.AmountCents(), totalCents)
	}
	if p.Amount.Currency() != currency {
		t.Fatalf("currency after Created = %s, want %s", p.Amount.Currency(), currency)
	}
	if p.BookingID != bookingID {
		t.Fatalf("payment BookingID = %s, want %s", p.BookingID, bookingID)
	}
	if p.GuestID != guestID {
		t.Fatalf("payment GuestID = %s, want %s", p.GuestID, guestID)
	}
	if p.GatewayRef == "" {
		t.Fatal("expected a gateway reference after authorization")
	}

	// A second Created delivery (at-least-once outbox) must NOT re-authorise:
	// the existing payment row is idempotently kept.
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCreated{
		BookingID: bookingID, ExperienceID: experienceID, HostID: hostID, GuestID: guestID,
		TotalCents: totalCents, Currency: currency, OccurredAt: time.Now().UTC(),
	})
	pReplay, _ := svc.GetForBooking(ctx, guestID, bookingID)
	if pReplay.GatewayRef != p.GatewayRef {
		t.Fatalf("redelivered Created changed gateway ref %q -> %q", p.GatewayRef, pReplay.GatewayRef)
	}

	// Confirmed → captured.
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingConfirmed{
		BookingID: bookingID, ExperienceID: experienceID, HostID: hostID, GuestID: guestID,
		OccurredAt: time.Now().UTC(),
	})
	p, _ = svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusCaptured {
		t.Fatalf("status after Confirmed = %s, want captured", p.Status)
	}

	// Cancelled → refunded in full (no cancellation-policy ladder for
	// experiences yet).
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCancelled{
		BookingID: bookingID, ExperienceID: experienceID, HostID: hostID, GuestID: guestID,
		CancelledBy: guestID, OccurredAt: time.Now().UTC(),
	})
	p, _ = svc.GetForBooking(ctx, guestID, bookingID)
	if p.Status != payment.StatusRefunded {
		t.Fatalf("status after Cancelled = %s, want refunded", p.Status)
	}
	if p.RefundedCents != totalCents {
		t.Fatalf("refunded after Cancelled = %d, want %d", p.RefundedCents, totalCents)
	}
}

// TestEventHandler_ExperienceBookingCancelled_NoPaymentIsSafe ensures a
// Cancelled event for a booking that never reached the payment subscriber
// (e.g. an old booking pre-dating S87) is a silent no-op rather than an
// error — the conditional pattern matches the property-booking case.
func TestEventHandler_ExperienceBookingCancelled_NoPaymentIsSafe(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPaymentRepository()
	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway(), memory.NewBookingRepository(), memory.NewPropertyRepository())
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	// No prior Created event: no payment row exists. The Cancelled handler
	// must not panic and must not create a phantom payment.
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCancelled{
		BookingID:   uuid.New(),
		GuestID:     uuid.New(),
		CancelledBy: uuid.New(),
		OccurredAt:  time.Now().UTC(),
	})
	// Nothing to assert beyond "didn't crash".
}

// TestEventHandler_AuthorizeRoutesThroughUnitOfWork — S123. When a UoW is
// wired into the service, the synchronous BookingRequested → authorize
// path must persist the payment row AND append a PaymentAuthorized
// outbox record in the same UoW. The in-memory UoW exposes the recorded
// outbox via the OutboxStore we passed in; we assert both happened.
func TestEventHandler_AuthorizeRoutesThroughUnitOfWork(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPaymentRepository()
	outbox := event.NewMemoryOutbox()
	uow := memory.NewUnitOfWork(nil, nil, nil, nil, nil, outbox, nil).WithPayments(repo)

	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway(), memory.NewBookingRepository(), memory.NewPropertyRepository()).
		WithUnitOfWork(uow)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	bookingID := uuid.New()
	guestID := uuid.New()
	dispatcher.Publish(ctx, event.BookingRequested{
		BookingID: bookingID, GuestID: guestID, TotalCents: 12000, Currency: "EUR",
	})

	// Payment row landed via the UoW's tx.Payments handle.
	p, err := svc.GetForBooking(ctx, guestID, bookingID)
	if err != nil {
		t.Fatalf("get payment: %v", err)
	}
	if p.Status != payment.StatusAuthorized {
		t.Fatalf("status = %s, want authorized", p.Status)
	}

	// AND a PaymentAuthorized record landed in the outbox in the same
	// UoW — the atomicity contract S123 is closing. Without the UoW
	// wiring, the legacy path skips the outbox append, so we'd see zero
	// pending events here.
	pending, err := outbox.FetchUnprocessed(ctx, 10)
	if err != nil {
		t.Fatalf("fetch outbox: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("outbox pending = %d, want 1 (one PaymentAuthorized per authorize UoW)", len(pending))
	}
	if pending[0].Name != (event.PaymentAuthorized{}).EventName() {
		t.Fatalf("outbox event_name = %q, want %q", pending[0].Name, (event.PaymentAuthorized{}).EventName())
	}
}

// TestEventHandler_CaptureRefundEmitPaymentLifecycleEvents — S123. Once a
// UoW is wired, the capture and refund transitions must each append a
// matching lifecycle event (PaymentCaptured / PaymentRefunded). Drives
// the full happy-path lifecycle and asserts the outbox holds exactly the
// three records (authorize, capture, refund) the path produces.
func TestEventHandler_CaptureRefundEmitPaymentLifecycleEvents(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPaymentRepository()
	outbox := event.NewMemoryOutbox()
	uow := memory.NewUnitOfWork(nil, nil, nil, nil, nil, outbox, nil).WithPayments(repo)

	svc := paymentapp.NewService(repo, infrapayment.NewFakeGateway(), memory.NewBookingRepository(), memory.NewPropertyRepository()).
		WithUnitOfWork(uow)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	bookingID := uuid.New()
	guestID := uuid.New()
	dispatcher.Publish(ctx, event.BookingRequested{BookingID: bookingID, GuestID: guestID, TotalCents: 10000, Currency: "EUR"})
	dispatcher.Publish(ctx, event.BookingConfirmed{BookingID: bookingID, GuestID: guestID})
	dispatcher.Publish(ctx, event.BookingCancelled{BookingID: bookingID, GuestID: guestID, RefundFraction: 0.5})

	pending, err := outbox.FetchUnprocessed(ctx, 10)
	if err != nil {
		t.Fatalf("fetch outbox: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("outbox pending = %d, want 3 (authorize + capture + refund)", len(pending))
	}
	want := []string{
		(event.PaymentAuthorized{}).EventName(),
		(event.PaymentCaptured{}).EventName(),
		(event.PaymentRefunded{}).EventName(),
	}
	for i, w := range want {
		if pending[i].Name != w {
			t.Errorf("outbox[%d].Name = %q, want %q", i, pending[i].Name, w)
		}
	}
}
