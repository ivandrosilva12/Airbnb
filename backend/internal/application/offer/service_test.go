package offerapp_test

import (
	"context"
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
