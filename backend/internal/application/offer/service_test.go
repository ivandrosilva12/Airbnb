package offerapp_test

import (
	"context"
	"errors"
	"testing"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/application/event"
	offerapp "github.com/airhost/backend/internal/application/offer"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

type stubVerifier struct{}

func (stubVerifier) IsVerified(context.Context, uuid.UUID) (bool, error) { return true, nil }

func days(n int) time.Time { return time.Now().UTC().AddDate(0, 0, n) }

func setup(t *testing.T) (*offerapp.Service, *memory.BookingRepository, *property.Property, uuid.UUID) {
	t.Helper()
	bookings := memory.NewBookingRepository()
	properties := memory.NewPropertyRepository()
	offers := memory.NewOfferRepository()
	outbox := event.NewMemoryOutbox()
	relay := event.NewDurablePublisher(outbox, event.NewDispatcher())
	uow := memory.NewUnitOfWork(bookings, nil, nil, nil, nil, outbox, relay)
	bookingSvc := bookingapp.NewService(bookings, properties, memory.NewBlockRepository(), memory.NewCouponRepository(), memory.NewPriceRuleRepository(), 0, stubVerifier{}, false, uow)
	svc := offerapp.NewService(offers, properties, bookingSvc)

	hostID := uuid.New()
	price, _ := shared.NewMoney(10000, "EUR") // 100.00/night
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	prop, err := property.NewProperty(hostID, "Flat", "", property.TypeApartment, addr, price, cleaning, 4, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	prop.AddPhoto("k", "http://x/k.jpg")
	_ = prop.Publish()
	if err := properties.Create(context.Background(), prop); err != nil {
		t.Fatalf("store property: %v", err)
	}
	return svc, bookings, prop, hostID
}

func TestOffer_SpecialOfferAcceptCreatesConfirmedBookingAtOfferPrice(t *testing.T) {
	svc, _, prop, hostID := setup(t)
	ctx := context.Background()
	guestID := uuid.New()

	o, err := svc.Create(ctx, offerapp.CreateInput{
		HostID: hostID, PropertyID: prop.ID, GuestID: guestID,
		CheckIn: days(1), CheckOut: days(4), Guests: 2, PriceCents: 8000, // 80.00/night special offer
	})
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}

	b, err := svc.Accept(ctx, guestID, o.ID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if b.Status != booking.StatusConfirmed {
		t.Fatalf("booking status = %s, want confirmed", b.Status)
	}
	// 3 nights * 80.00 (offer price, not the 100.00 listing price).
	if b.Pricing.Subtotal.AmountCents() != 24000 {
		t.Fatalf("subtotal = %d, want 24000 (offer price)", b.Pricing.Subtotal.AmountCents())
	}

	// The offer can't be accepted twice.
	if _, err := svc.Accept(ctx, guestID, o.ID); err == nil {
		t.Fatal("expected a second accept to fail")
	}
}

// fakeOfferMetrics records IncOffer calls so the test can assert the
// counter was bumped with the expected EventName label (S113).
type fakeOfferMetrics struct {
	calls []string
}

func (f *fakeOfferMetrics) IncOffer(eventName string) {
	f.calls = append(f.calls, eventName)
}

// TestService_Create_IncrementsMetric verifies that a successful Create
// bumps the OffersTotal counter via the OfferMetrics sink, labeled with
// the event's EventName ("offer.created"). The publisher is wired so
// emit reaches the metrics branch too, mirroring main.go's wiring
// (S113 — follow-on to S99/WF-GAP-008).
func TestService_Create_IncrementsMetric(t *testing.T) {
	svc, _, prop, hostID := setup(t)
	metrics := &fakeOfferMetrics{}
	svc = svc.WithPublisher(event.NewDispatcher()).WithMetrics(metrics)
	ctx := context.Background()

	if _, err := svc.Create(ctx, offerapp.CreateInput{
		HostID: hostID, PropertyID: prop.ID, GuestID: uuid.New(),
		CheckIn: days(1), CheckOut: days(3), Guests: 1,
	}); err != nil {
		t.Fatalf("create offer: %v", err)
	}

	if len(metrics.calls) != 1 {
		t.Fatalf("IncOffer call count = %d, want 1 (calls = %v)", len(metrics.calls), metrics.calls)
	}
	if metrics.calls[0] != "offer.created" {
		t.Fatalf("IncOffer label = %q, want %q", metrics.calls[0], "offer.created")
	}
}

// outboxFixture wires a fresh UoW + outbox + dispatcher so the
// transactional path can be exercised end-to-end. The captured slice
// records every event the dispatcher fans out — proving the relay
// drained exactly what landed in the outbox.
type outboxFixture struct {
	svc        *offerapp.Service
	outbox     event.OutboxStore
	dispatcher *event.Dispatcher
	captured   *[]event.Event
}

// setupOutbox builds the offer service on the S155 transactional path
// (UoW + outbox + dispatcher), seeds a published property owned by
// hostID and returns the fixture along with the property.
func setupOutbox(t *testing.T) (outboxFixture, *property.Property, uuid.UUID) {
	t.Helper()
	bookings := memory.NewBookingRepository()
	properties := memory.NewPropertyRepository()
	offers := memory.NewOfferRepository()
	outbox := event.NewMemoryOutbox()
	dispatcher := event.NewDispatcher()
	var captured []event.Event
	dispatcher.Subscribe(func(_ context.Context, ev event.Event) {
		captured = append(captured, ev)
	})
	relay := event.NewDurablePublisher(outbox, dispatcher)
	uow := memory.NewUnitOfWork(bookings, nil, nil, nil, nil, outbox, relay)
	bookingSvc := bookingapp.NewService(bookings, properties, memory.NewBlockRepository(), memory.NewCouponRepository(), memory.NewPriceRuleRepository(), 0, stubVerifier{}, false, uow)
	svc := offerapp.NewService(offers, properties, bookingSvc).
		WithPublisher(dispatcher).
		WithUnitOfWork(uow)

	hostID := uuid.New()
	price, _ := shared.NewMoney(10000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	prop, err := property.NewProperty(hostID, "Flat", "", property.TypeApartment, addr, price, cleaning, 4, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	prop.AddPhoto("k", "http://x/k.jpg")
	_ = prop.Publish()
	if err := properties.Create(context.Background(), prop); err != nil {
		t.Fatalf("store property: %v", err)
	}
	return outboxFixture{svc: svc, outbox: outbox, dispatcher: dispatcher, captured: &captured}, prop, hostID
}

// TestService_Create_AppendsOutboxEventInSameTx proves S155's contract
// for the Create flow: when a UoW is wired, the offer Create+publish
// becomes a single atomic step — the OfferCreated record lands in the
// outbox (and is then dispatched, marking it processed). The legacy
// in-process publish path is bypassed; the in-tx outbox append carries
// the event instead, so a crash between the row write and the publish
// can no longer drop it.
func TestService_Create_AppendsOutboxEventInSameTx(t *testing.T) {
	f, prop, hostID := setupOutbox(t)
	ctx := context.Background()
	guestID := uuid.New()

	o, err := f.svc.Create(ctx, offerapp.CreateInput{
		HostID: hostID, PropertyID: prop.ID, GuestID: guestID,
		CheckIn: days(1), CheckOut: days(3), Guests: 1,
	})
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}

	// The dispatcher sees exactly one OfferCreated — proving the relay
	// drained the just-committed outbox record.
	names := make([]string, 0, len(*f.captured))
	for _, e := range *f.captured {
		names = append(names, e.EventName())
	}
	if len(names) != 1 || names[0] != (event.OfferCreated{}).EventName() {
		t.Fatalf("expected dispatcher to see one OfferCreated, got %v", names)
	}
	// Once dispatched the relay marks the record processed, so a
	// follow-up unprocessed sweep is empty — confirming the event
	// flowed through the outbox first, not the legacy direct path.
	recs, err := f.outbox.FetchUnprocessed(ctx, 100)
	if err != nil {
		t.Fatalf("fetch outbox: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected outbox drained after dispatch, got %d unprocessed", len(recs))
	}
	if o.ID == uuid.Nil {
		t.Fatal("expected offer to have an ID")
	}
}

// TestService_Decline_AppendsOutboxEventInSameTx proves the Decline
// transition emits OfferDeclined through the outbox (S155).
func TestService_Decline_AppendsOutboxEventInSameTx(t *testing.T) {
	f, prop, hostID := setupOutbox(t)
	ctx := context.Background()
	guestID := uuid.New()

	o, err := f.svc.Create(ctx, offerapp.CreateInput{
		HostID: hostID, PropertyID: prop.ID, GuestID: guestID,
		CheckIn: days(1), CheckOut: days(3), Guests: 1,
	})
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if err := f.svc.Decline(ctx, guestID, o.ID); err != nil {
		t.Fatalf("decline: %v", err)
	}

	names := make([]string, 0, len(*f.captured))
	for _, e := range *f.captured {
		names = append(names, e.EventName())
	}
	wantCreated := (event.OfferCreated{}).EventName()
	wantDeclined := (event.OfferDeclined{}).EventName()
	if len(names) != 2 || names[0] != wantCreated || names[1] != wantDeclined {
		t.Fatalf("expected [%s, %s], got %v", wantCreated, wantDeclined, names)
	}
	recs, err := f.outbox.FetchUnprocessed(ctx, 100)
	if err != nil {
		t.Fatalf("fetch outbox: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected outbox drained after dispatch, got %d unprocessed", len(recs))
	}
}

// TestService_Withdraw_AppendsOutboxEventInSameTx proves the Withdraw
// transition emits OfferWithdrawn through the outbox (S155).
func TestService_Withdraw_AppendsOutboxEventInSameTx(t *testing.T) {
	f, prop, hostID := setupOutbox(t)
	ctx := context.Background()
	guestID := uuid.New()

	o, err := f.svc.Create(ctx, offerapp.CreateInput{
		HostID: hostID, PropertyID: prop.ID, GuestID: guestID,
		CheckIn: days(1), CheckOut: days(3), Guests: 1,
	})
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if err := f.svc.Withdraw(ctx, hostID, o.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	names := make([]string, 0, len(*f.captured))
	for _, e := range *f.captured {
		names = append(names, e.EventName())
	}
	wantCreated := (event.OfferCreated{}).EventName()
	wantWithdrawn := (event.OfferWithdrawn{}).EventName()
	if len(names) != 2 || names[0] != wantCreated || names[1] != wantWithdrawn {
		t.Fatalf("expected [%s, %s], got %v", wantCreated, wantWithdrawn, names)
	}
}

// TestService_Create_RollbackSuppressesEventAndMetric proves the second
// half of S155's contract: when the UoW rejects the commit, the
// OfferCreated event must NOT reach any subscriber AND the metric
// counter must NOT be bumped — otherwise the rollback is observable.
// The in-memory UoW's WithCommitError discards the recorded events
// before dispatching, mirroring what production does when the DB
// commit fails.
func TestService_Create_RollbackSuppressesEventAndMetric(t *testing.T) {
	bookings := memory.NewBookingRepository()
	properties := memory.NewPropertyRepository()
	offers := memory.NewOfferRepository()
	outbox := event.NewMemoryOutbox()
	dispatcher := event.NewDispatcher()
	var captured []event.Event
	dispatcher.Subscribe(func(_ context.Context, ev event.Event) {
		captured = append(captured, ev)
	})
	relay := event.NewDurablePublisher(outbox, dispatcher)
	commitErr := errors.New("simulated commit failure")
	uow := memory.NewUnitOfWork(bookings, nil, nil, nil, nil, outbox, relay).WithCommitError(commitErr)
	bookingSvc := bookingapp.NewService(bookings, properties, memory.NewBlockRepository(), memory.NewCouponRepository(), memory.NewPriceRuleRepository(), 0, stubVerifier{}, false, uow)
	metrics := &fakeOfferMetrics{}
	svc := offerapp.NewService(offers, properties, bookingSvc).
		WithPublisher(dispatcher).
		WithUnitOfWork(uow).
		WithMetrics(metrics)

	hostID := uuid.New()
	price, _ := shared.NewMoney(10000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	prop, err := property.NewProperty(hostID, "Flat", "", property.TypeApartment, addr, price, cleaning, 4, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	prop.AddPhoto("k", "http://x/k.jpg")
	_ = prop.Publish()
	if err := properties.Create(context.Background(), prop); err != nil {
		t.Fatalf("store property: %v", err)
	}

	_, err = svc.Create(context.Background(), offerapp.CreateInput{
		HostID: hostID, PropertyID: prop.ID, GuestID: uuid.New(),
		CheckIn: days(1), CheckOut: days(3), Guests: 1,
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("expected commit failure surfaced to caller, got %v", err)
	}
	// Critically: no subscriber should have observed an event — the
	// whole point of the UoW is that a rollback drops the event too.
	if len(captured) != 0 {
		names := make([]string, 0, len(captured))
		for _, e := range captured {
			names = append(names, e.EventName())
		}
		t.Fatalf("expected zero dispatched events on rollback, got %v", names)
	}
	// And the metric must not have been bumped — bumpMetric only runs
	// after uow.Run returns nil.
	if len(metrics.calls) != 0 {
		t.Fatalf("expected metric not bumped on rollback, got %v", metrics.calls)
	}
}

func TestOffer_OnlyHostCreatesOnlyGuestActs(t *testing.T) {
	svc, _, prop, hostID := setup(t)
	ctx := context.Background()
	guestID := uuid.New()

	// A non-owner cannot send an offer for this listing.
	if _, err := svc.Create(ctx, offerapp.CreateInput{
		HostID: uuid.New(), PropertyID: prop.ID, GuestID: guestID, CheckIn: days(1), CheckOut: days(3), Guests: 1,
	}); err != shared.ErrForbidden {
		t.Fatalf("non-owner create err = %v, want ErrForbidden", err)
	}

	o, err := svc.Create(ctx, offerapp.CreateInput{
		HostID: hostID, PropertyID: prop.ID, GuestID: guestID, CheckIn: days(1), CheckOut: days(3), Guests: 1,
	})
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if o.Kind != "pre_approval" {
		t.Fatalf("kind = %s, want pre_approval (no custom price)", o.Kind)
	}

	// A stranger cannot accept someone else's offer.
	if _, err := svc.Accept(ctx, uuid.New(), o.ID); err != shared.ErrForbidden {
		t.Fatalf("stranger accept err = %v, want ErrForbidden", err)
	}
	// The guest declines; it can no longer be accepted.
	if err := svc.Decline(ctx, guestID, o.ID); err != nil {
		t.Fatalf("decline: %v", err)
	}
	if _, err := svc.Accept(ctx, guestID, o.ID); err == nil {
		t.Fatal("expected accepting a declined offer to fail")
	}
}
