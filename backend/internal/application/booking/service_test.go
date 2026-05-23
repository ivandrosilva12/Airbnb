package bookingapp_test

import (
	"context"
	"testing"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

type fixture struct {
	svc        *bookingapp.Service
	bookings   *memory.BookingRepository
	properties *memory.PropertyRepository
	hostID     uuid.UUID
	guestID    uuid.UUID
	prop       *property.Property
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	bookings := memory.NewBookingRepository()
	properties := memory.NewPropertyRepository()
	svc := bookingapp.NewService(bookings, properties)

	hostID := uuid.New()
	price, _ := shared.NewMoney(10000, "EUR") // 100.00/night
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	prop, err := property.NewProperty(hostID, "Sunny flat", "", property.TypeApartment, addr, price, 4, 2, 2, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	prop.AddPhoto("k", "http://x/k.jpg")
	if err := prop.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := properties.Create(context.Background(), prop); err != nil {
		t.Fatalf("store property: %v", err)
	}

	return &fixture{svc: svc, bookings: bookings, properties: properties, hostID: hostID, guestID: uuid.New(), prop: prop}
}

func days(n int) time.Time { return time.Now().UTC().AddDate(0, 0, n) }

func TestCreate_HappyPathDerivesPrice(t *testing.T) {
	f := newFixture(t)
	b, err := f.svc.Create(context.Background(), bookingapp.CreateInput{
		GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 2,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.TotalPrice.AmountCents() != 30000 { // 3 nights * 100.00
		t.Errorf("total = %d, want 30000", b.TotalPrice.AmountCents())
	}
}

func TestCreate_RejectsDoubleBooking(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(5), Guests: 1}); err != nil {
		t.Fatalf("first booking: %v", err)
	}
	_, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: uuid.New(), PropertyID: f.prop.ID, CheckIn: days(3), CheckOut: days(7), Guests: 1})
	if err == nil {
		t.Fatal("expected overlap to be rejected")
	}
}

func TestCreate_AllowsAdjacentDates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 1}); err != nil {
		t.Fatalf("first booking: %v", err)
	}
	// Check-in on the previous booking's check-out day must be allowed (half-open).
	if _, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: uuid.New(), PropertyID: f.prop.ID, CheckIn: days(4), CheckOut: days(6), Guests: 1}); err != nil {
		t.Fatalf("adjacent booking should be allowed, got: %v", err)
	}
}

func TestCreate_HostCannotBookOwnProperty(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Create(context.Background(), bookingapp.CreateInput{GuestID: f.hostID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(2), Guests: 1})
	if err == nil {
		t.Fatal("expected host booking own property to be rejected")
	}
}

func TestCreate_RejectsOverCapacity(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Create(context.Background(), bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(2), Guests: 99})
	if err == nil {
		t.Fatal("expected over-capacity booking to be rejected")
	}
}

func TestComplete_OnlyHostAndAfterCheckout(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	b, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(3), Guests: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.svc.Confirm(ctx, f.hostID, b.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	// A stranger cannot complete.
	if _, err := f.svc.Complete(ctx, uuid.New(), b.ID); err != shared.ErrForbidden {
		t.Errorf("expected ErrForbidden for non-host, got %v", err)
	}
	// Cannot complete before check-out.
	if _, err := f.svc.Complete(ctx, f.hostID, b.ID); err == nil {
		t.Error("expected completion before checkout to fail")
	}
}

func TestAvailability_ReturnsBookedRanges(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(2), CheckOut: days(5), Guests: 1}); err != nil {
		t.Fatalf("create: %v", err)
	}
	ranges, err := f.svc.Availability(ctx, f.prop.ID, days(0), days(30))
	if err != nil {
		t.Fatalf("availability: %v", err)
	}
	if len(ranges) != 1 {
		t.Fatalf("expected 1 booked range, got %d", len(ranges))
	}
}
