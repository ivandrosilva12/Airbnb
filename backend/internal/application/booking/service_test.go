package bookingapp_test

import (
	"context"
	"testing"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/coupon"
	"github.com/airhost/backend/internal/domain/pricerule"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

type fixture struct {
	svc        *bookingapp.Service
	bookings   *memory.BookingRepository
	properties *memory.PropertyRepository
	coupons    *memory.CouponRepository
	priceRules *memory.PriceRuleRepository
	hostID     uuid.UUID
	guestID    uuid.UUID
	prop       *property.Property
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	bookings := memory.NewBookingRepository()
	properties := memory.NewPropertyRepository()
	coupons := memory.NewCouponRepository()
	priceRules := memory.NewPriceRuleRepository()
	outbox := event.NewMemoryOutbox()
	relay := event.NewDurablePublisher(outbox, event.NewDispatcher())
	uow := memory.NewUnitOfWork(bookings, nil, nil, outbox, relay)
	svc := bookingapp.NewService(bookings, properties, memory.NewBlockRepository(), coupons, priceRules, 0, stubVerifier{}, false, uow)

	hostID := uuid.New()
	price, _ := shared.NewMoney(10000, "EUR") // 100.00/night
	cleaning, _ := shared.NewMoney(0, "EUR")  // no cleaning fee in this fixture
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	prop, err := property.NewProperty(hostID, "Sunny flat", "", property.TypeApartment, addr, price, cleaning, 4, 2, 2, 1, nil)
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

	return &fixture{svc: svc, bookings: bookings, properties: properties, coupons: coupons, priceRules: priceRules, hostID: hostID, guestID: uuid.New(), prop: prop}
}

func days(n int) time.Time { return time.Now().UTC().AddDate(0, 0, n) }

// stubVerifier is a test IdentityVerifier with a fixed set of verified users.
type stubVerifier struct{ verified map[uuid.UUID]bool }

func (s stubVerifier) IsVerified(_ context.Context, id uuid.UUID) (bool, error) {
	return s.verified[id], nil
}

func TestCreate_KYCGatingBlocksUnverifiedGuest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// A second booking service over the same repos, with KYC gating enabled.
	outbox := event.NewMemoryOutbox()
	relay := event.NewDurablePublisher(outbox, event.NewDispatcher())
	uow := memory.NewUnitOfWork(f.bookings, nil, nil, outbox, relay)
	verifier := stubVerifier{verified: map[uuid.UUID]bool{}}
	gated := bookingapp.NewService(f.bookings, f.properties, memory.NewBlockRepository(), f.coupons, memory.NewPriceRuleRepository(), 0, verifier, true, uow)

	// An unverified guest is refused.
	if _, err := gated.Create(ctx, bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 1}); err == nil {
		t.Fatal("expected an unverified guest to be blocked by KYC gating")
	}
	// Once verified, the booking goes through.
	verifier.verified[f.guestID] = true
	if _, err := gated.Create(ctx, bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 1}); err != nil {
		t.Fatalf("a verified guest should be able to book: %v", err)
	}
}

func TestCreate_HappyPathDerivesPrice(t *testing.T) {
	f := newFixture(t)
	b, err := f.svc.Create(context.Background(), bookingapp.CreateInput{
		GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 2,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.TotalPrice().AmountCents() != 30000 { // 3 nights * 100.00, no fees in fixture
		t.Errorf("total = %d, want 30000", b.TotalPrice().AmountCents())
	}
}

func TestCreate_AppliesCouponAndRedeems(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	c, err := coupon.NewPercentage("SAVE10", 0.10, 0, 5, nil)
	if err != nil {
		t.Fatalf("new coupon: %v", err)
	}
	if err := f.coupons.Create(ctx, c); err != nil {
		t.Fatalf("store coupon: %v", err)
	}

	b, err := f.svc.Create(ctx, bookingapp.CreateInput{
		GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 2,
		CouponCode: "save10", // case-insensitive
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 3 nights * 100.00 = 30000; 10% promo => 3000 discount, no fees in fixture.
	if b.Pricing.Discount.AmountCents() != 3000 {
		t.Errorf("discount = %d, want 3000", b.Pricing.Discount.AmountCents())
	}
	if b.TotalPrice().AmountCents() != 27000 {
		t.Errorf("total = %d, want 27000", b.TotalPrice().AmountCents())
	}
	// The redemption was recorded.
	reloaded, err := f.coupons.FindByCode(ctx, "SAVE10")
	if err != nil {
		t.Fatalf("reload coupon: %v", err)
	}
	if reloaded.Redemptions != 1 {
		t.Errorf("redemptions = %d, want 1", reloaded.Redemptions)
	}
}

func TestCreate_StayRulesAndExtraGuestFee(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	fee, _ := shared.NewMoney(2000, "EUR")     // 20.00 per extra guest, per night
	deposit, _ := shared.NewMoney(5000, "EUR") // 50.00 refundable hold
	if err := f.prop.SetStayRules(3, 0, 1, fee, deposit); err != nil {
		t.Fatalf("set stay rules: %v", err)
	}
	if err := f.properties.Update(ctx, f.prop); err != nil {
		t.Fatalf("update property: %v", err)
	}

	// A 2-night stay is below the 3-night minimum.
	if _, err := f.svc.Create(ctx, bookingapp.CreateInput{
		GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(3), Guests: 1,
	}); err == nil {
		t.Fatal("expected a 2-night stay to be rejected (min 3 nights)")
	}

	// A 3-night stay with 3 guests: 2 extra guests * 20.00 * 3 nights = 120.00,
	// plus a 50.00 deposit on top of the 300.00 accommodation.
	b, err := f.svc.Create(ctx, bookingapp.CreateInput{
		GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 3,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.Pricing.ExtraGuestFee.AmountCents() != 12000 {
		t.Errorf("extra-guest fee = %d, want 12000", b.Pricing.ExtraGuestFee.AmountCents())
	}
	if b.Pricing.SecurityDeposit.AmountCents() != 5000 {
		t.Errorf("deposit = %d, want 5000", b.Pricing.SecurityDeposit.AmountCents())
	}
	if b.TotalPrice().AmountCents() != 47000 { // 30000 + 12000 + 5000
		t.Errorf("total = %d, want 47000", b.TotalPrice().AmountCents())
	}
}

func TestCreate_InvalidCouponFailsBooking(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Create(context.Background(), bookingapp.CreateInput{
		GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 2,
		CouponCode: "NOPE",
	})
	if err == nil {
		t.Fatal("expected an unknown coupon code to fail the booking")
	}
}

func TestCreate_RequestToBookStaysPending(t *testing.T) {
	f := newFixture(t)
	b, err := f.svc.Create(context.Background(), bookingapp.CreateInput{
		GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.Status != booking.StatusPending {
		t.Fatalf("status = %q, want pending (request to book)", b.Status)
	}
}

func TestCreate_InstantBookAutoConfirms(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.prop.SetInstantBook(true)
	if err := f.properties.Update(ctx, f.prop); err != nil {
		t.Fatalf("enable instant book: %v", err)
	}
	b, err := f.svc.Create(ctx, bookingapp.CreateInput{
		GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 2,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.Status != booking.StatusConfirmed {
		t.Fatalf("returned status = %q, want confirmed (instant book)", b.Status)
	}
	// The reservation is persisted confirmed, so no host approval step remains.
	stored, err := f.bookings.FindByID(ctx, b.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if stored.Status != booking.StatusConfirmed {
		t.Fatalf("persisted status = %q, want confirmed", stored.Status)
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

func TestModify_RepricesAndReschedules(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	b, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 3 nights -> 30000. Extend the stay to 5 nights and add a guest.
	mod, err := f.svc.Modify(ctx, bookingapp.ModifyInput{ActorID: f.guestID, BookingID: b.ID, CheckIn: days(1), CheckOut: days(6), Guests: 3})
	if err != nil {
		t.Fatalf("modify: %v", err)
	}
	if mod.Dates.Nights() != 5 {
		t.Errorf("nights = %d, want 5", mod.Dates.Nights())
	}
	if mod.Guests != 3 {
		t.Errorf("guests = %d, want 3", mod.Guests)
	}
	if mod.TotalPrice().AmountCents() != 50000 { // 5 nights * 100.00, no fees in fixture
		t.Errorf("total = %d, want 50000", mod.TotalPrice().AmountCents())
	}
	stored, err := f.bookings.FindByID(ctx, b.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if stored.Dates.Nights() != 5 || stored.Guests != 3 {
		t.Errorf("persisted nights/guests = %d/%d, want 5/3", stored.Dates.Nights(), stored.Guests)
	}
}

func TestModify_RejectsConfirmedBooking(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.prop.SetInstantBook(true)
	if err := f.properties.Update(ctx, f.prop); err != nil {
		t.Fatalf("enable instant book: %v", err)
	}
	b, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.svc.Modify(ctx, bookingapp.ModifyInput{ActorID: f.guestID, BookingID: b.ID, CheckIn: days(2), CheckOut: days(5), Guests: 1}); err == nil {
		t.Fatal("expected modifying a confirmed (instant-book) booking to be rejected")
	}
}

func TestModify_OnlyGuestMayModify(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	b, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.svc.Modify(ctx, bookingapp.ModifyInput{ActorID: uuid.New(), BookingID: b.ID, CheckIn: days(2), CheckOut: days(5), Guests: 1}); err != shared.ErrForbidden {
		t.Errorf("expected ErrForbidden for non-guest, got %v", err)
	}
}

func TestModify_RejectsOverlapButIgnoresSelf(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mine, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: days(1), CheckOut: days(4), Guests: 1})
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	// Another guest occupies days 6..9.
	if _, err := f.svc.Create(ctx, bookingapp.CreateInput{GuestID: uuid.New(), PropertyID: f.prop.ID, CheckIn: days(6), CheckOut: days(9), Guests: 1}); err != nil {
		t.Fatalf("create other: %v", err)
	}
	// Moving my stay onto the other booking's range must be rejected.
	if _, err := f.svc.Modify(ctx, bookingapp.ModifyInput{ActorID: f.guestID, BookingID: mine.ID, CheckIn: days(5), CheckOut: days(8), Guests: 1}); err == nil {
		t.Fatal("expected overlap with another booking to be rejected")
	}
	// Extending into days my own booking already (partly) covers must succeed —
	// the availability check ignores the booking being modified.
	if _, err := f.svc.Modify(ctx, bookingapp.ModifyInput{ActorID: f.guestID, BookingID: mine.ID, CheckIn: days(1), CheckOut: days(5), Guests: 1}); err != nil {
		t.Fatalf("self-overlapping extension should be allowed, got: %v", err)
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

// nextMonday returns the next Monday at midnight UTC, so a stay starting on
// that date covers a known weekly pattern: Mon-Tue-Wed-Thu-Fri-Sat-Sun.
func nextMonday() time.Time {
	t := time.Now().UTC().Truncate(24 * time.Hour)
	for t.Weekday() != time.Monday {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func TestCreate_SeasonalAndWeekendPricing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Listing nightly = 100.00. Weekend price = 150.00 (Fri/Sat). Plus a
	// seasonal rule that overrides the Wednesday night to 200.00.
	f.prop.SetPricingPolicy(property.PricingPolicy{WeekendPriceCents: 15000})
	if err := f.properties.Update(ctx, f.prop); err != nil {
		t.Fatalf("update property: %v", err)
	}
	mon := nextMonday()
	wed := mon.AddDate(0, 0, 2)
	rule, err := pricerule.New(f.prop.ID, wed, wed.AddDate(0, 0, 1), 20000, "EUR", "Conference")
	if err != nil {
		t.Fatalf("new rule: %v", err)
	}
	if err := f.priceRules.Create(ctx, rule); err != nil {
		t.Fatalf("store rule: %v", err)
	}

	// 7-night stay: Mon, Tue, Wed*, Thu, Fri†, Sat†, Sun
	// * Wed = seasonal override (200.00); † Fri/Sat = weekend (150.00 each).
	// Subtotal = 100 + 100 + 200 + 100 + 150 + 150 + 100 = 900.00 (90000c).
	// The listing's default weekly discount applies at 7 nights, but the fixture
	// did not configure one, so it stays at zero.
	b, err := f.svc.Create(ctx, bookingapp.CreateInput{
		GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: mon, CheckOut: mon.AddDate(0, 0, 7), Guests: 1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := b.Pricing.Subtotal.AmountCents(); got != 90000 {
		t.Errorf("subtotal = %d, want 90000 (100+100+200+100+150+150+100)", got)
	}
	if got := b.TotalPrice().AmountCents(); got != 90000 {
		t.Errorf("total = %d, want 90000 (no discounts/fees in fixture)", got)
	}
}

func TestCreate_SpecialOfferOverridesRules(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Configure a weekend override; a seasonal rule too, just to prove neither
	// kicks in when a per-night special offer is supplied.
	f.prop.SetPricingPolicy(property.PricingPolicy{WeekendPriceCents: 15000})
	if err := f.properties.Update(ctx, f.prop); err != nil {
		t.Fatalf("update property: %v", err)
	}
	mon := nextMonday()
	rule, err := pricerule.New(f.prop.ID, mon, mon.AddDate(0, 0, 7), 30000, "EUR", "Bogus")
	if err != nil {
		t.Fatalf("new rule: %v", err)
	}
	if err := f.priceRules.Create(ctx, rule); err != nil {
		t.Fatalf("store rule: %v", err)
	}

	// A 3-night offer at 80.00/night must price out at exactly 240.00.
	b, err := f.svc.Create(ctx, bookingapp.CreateInput{
		GuestID: f.guestID, PropertyID: f.prop.ID, CheckIn: mon, CheckOut: mon.AddDate(0, 0, 3), Guests: 1,
		OverridePricePerNightCents: 8000,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := b.Pricing.Subtotal.AmountCents(); got != 24000 {
		t.Errorf("subtotal = %d, want 24000 (special offer overrides rules and weekend)", got)
	}
}
