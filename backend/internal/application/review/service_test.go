package reviewapp_test

import (
	"context"
	"testing"
	"time"

	reviewapp "github.com/airhost/backend/internal/application/review"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

type reviewFixture struct {
	svc        *reviewapp.Service
	bookings   *memory.BookingRepository
	properties *memory.PropertyRepository
	reviews    *memory.ReviewRepository
}

func setup(t *testing.T) reviewFixture {
	t.Helper()
	bookings := memory.NewBookingRepository()
	reviews := memory.NewReviewRepository()
	properties := memory.NewPropertyRepository()
	return reviewFixture{
		svc:        reviewapp.NewService(reviews, bookings, properties),
		bookings:   bookings,
		properties: properties,
		reviews:    reviews,
	}
}

// makeProperty stores a published property owned by hostID and returns it.
func makeProperty(t *testing.T, props *memory.PropertyRepository, hostID uuid.UUID) *property.Property {
	t.Helper()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	p, err := property.NewProperty(hostID, "Flat", "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	if err := props.Create(context.Background(), p); err != nil {
		t.Fatalf("store property: %v", err)
	}
	return p
}

func makeBooking(t *testing.T, guestID, propertyID uuid.UUID, status booking.Status) *booking.Booking {
	t.Helper()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	start := time.Now().UTC().AddDate(0, 0, 1)
	dr, err := booking.NewDateRange(start, start.AddDate(0, 0, 2))
	if err != nil {
		t.Fatalf("date range: %v", err)
	}
	b, err := booking.NewBooking(propertyID, guestID, dr, 1, price, cleaning, 0, booking.Discounts{})
	if err != nil {
		t.Fatalf("new booking: %v", err)
	}
	switch status {
	case booking.StatusConfirmed:
		_ = b.Confirm()
	case booking.StatusCompleted:
		_ = b.Confirm()
		_ = b.Complete()
	case booking.StatusCancelled:
		_ = b.Cancel()
	}
	return b
}

func TestReview_RequiresCompletedBooking(t *testing.T) {
	f := setup(t)
	guestID := uuid.New()
	b := makeBooking(t, guestID, uuid.New(), booking.StatusConfirmed)
	_ = f.bookings.Create(context.Background(), b)

	_, err := f.svc.Create(context.Background(), reviewapp.CreateInput{GuestID: guestID, BookingID: b.ID, Rating: 5})
	if err == nil {
		t.Fatal("expected review of non-completed booking to fail")
	}
}

func TestReview_OnlyGuestMayReview(t *testing.T) {
	f := setup(t)
	guestID := uuid.New()
	b := makeBooking(t, guestID, uuid.New(), booking.StatusCompleted)
	_ = f.bookings.Create(context.Background(), b)

	_, err := f.svc.Create(context.Background(), reviewapp.CreateInput{GuestID: uuid.New(), BookingID: b.ID, Rating: 4})
	if err != shared.ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestReview_HappyPathThenNoDuplicates(t *testing.T) {
	f := setup(t)
	guestID := uuid.New()
	b := makeBooking(t, guestID, uuid.New(), booking.StatusCompleted)
	_ = f.bookings.Create(context.Background(), b)

	rv, err := f.svc.Create(context.Background(), reviewapp.CreateInput{GuestID: guestID, BookingID: b.ID, Rating: 5, Comment: "Lovely"})
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	if rv.PropertyID != b.PropertyID {
		t.Error("review property id should match booking")
	}

	_, err = f.svc.Create(context.Background(), reviewapp.CreateInput{GuestID: guestID, BookingID: b.ID, Rating: 3})
	if err == nil {
		t.Fatal("expected duplicate review to fail")
	}

	summary, err := f.svc.Summary(context.Background(), b.PropertyID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.Count != 1 || summary.AverageRating != 5 {
		t.Errorf("summary = %+v, want count 1 avg 5", summary)
	}
}

func TestReview_RefreshesPropertyRating(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	prop := makeProperty(t, f.properties, uuid.New())
	guestID := uuid.New()
	b := makeBooking(t, guestID, prop.ID, booking.StatusCompleted)
	_ = f.bookings.Create(ctx, b)

	if _, err := f.svc.Create(ctx, reviewapp.CreateInput{GuestID: guestID, BookingID: b.ID, Rating: 4, Comment: "Nice"}); err != nil {
		t.Fatalf("create review: %v", err)
	}

	updated, err := f.properties.FindByID(ctx, prop.ID)
	if err != nil {
		t.Fatalf("find property: %v", err)
	}
	if updated.ReviewCount != 1 || updated.AverageRating != 4 {
		t.Errorf("property rating = %v (%d reviews), want 4.0 (1)", updated.AverageRating, updated.ReviewCount)
	}
}

func TestReview_RecomputesHostSuperhost(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	hostID := uuid.New()
	propA := makeProperty(t, f.properties, hostID)
	propB := makeProperty(t, f.properties, hostID) // a second listing of the same host

	// Seed 9 prior 5-star property reviews on propA. They are reflected in propA's
	// cached aggregate only once a refresh runs (i.e. when the 10th is published).
	for i := 0; i < 9; i++ {
		rv, err := review.NewPropertyReview(uuid.New(), propA.ID, uuid.New(), 5, "")
		if err != nil {
			t.Fatalf("seed review: %v", err)
		}
		if err := f.reviews.Create(ctx, rv); err != nil {
			t.Fatalf("store seed review: %v", err)
		}
	}

	// Below the threshold there is no badge yet.
	if before, _ := f.properties.FindByID(ctx, propB.ID); before.HostIsSuperhost {
		t.Fatal("host should not be a superhost before crossing the threshold")
	}

	// Publishing the 10th 5-star review (avg 5.0, count 10) crosses the threshold.
	guestID := uuid.New()
	b := makeBooking(t, guestID, propA.ID, booking.StatusCompleted)
	_ = f.bookings.Create(ctx, b)
	if _, err := f.svc.Create(ctx, reviewapp.CreateInput{GuestID: guestID, BookingID: b.ID, Rating: 5}); err != nil {
		t.Fatalf("create 10th review: %v", err)
	}

	// The flag is fanned out across every listing of the host, including propB
	// which itself has no reviews.
	for _, id := range []uuid.UUID{propA.ID, propB.ID} {
		p, err := f.properties.FindByID(ctx, id)
		if err != nil {
			t.Fatalf("find property: %v", err)
		}
		if !p.HostIsSuperhost {
			t.Errorf("property %v should reflect host superhost status", id)
		}
	}
}

func TestPendingForGuest(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	guestID := uuid.New()
	prop := makeProperty(t, f.properties, uuid.New())

	// A completed, unreviewed stay is pending.
	done := makeBooking(t, guestID, prop.ID, booking.StatusCompleted)
	_ = f.bookings.Create(ctx, done)
	// A confirmed (not completed) stay is not pending.
	confirmed := makeBooking(t, guestID, prop.ID, booking.StatusConfirmed)
	_ = f.bookings.Create(ctx, confirmed)
	// Another guest's completed stay is not mine.
	other := makeBooking(t, uuid.New(), prop.ID, booking.StatusCompleted)
	_ = f.bookings.Create(ctx, other)

	pending, err := f.svc.PendingForGuest(ctx, guestID, shared.NewPage(50, 0))
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	if pending[0].BookingID != done.ID || pending[0].PropertyTitle != "Flat" {
		t.Fatalf("unexpected pending entry: %+v", pending[0])
	}

	// After reviewing it, nothing is pending.
	if _, err := f.svc.Create(ctx, reviewapp.CreateInput{GuestID: guestID, BookingID: done.ID, Rating: 5}); err != nil {
		t.Fatalf("create review: %v", err)
	}
	pending, err = f.svc.PendingForGuest(ctx, guestID, shared.NewPage(50, 0))
	if err != nil {
		t.Fatalf("pending after review: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after review = %d, want 0", len(pending))
	}
}

func TestGuestReview_OnlyHostAfterCompletion(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	hostID := uuid.New()
	guestID := uuid.New()
	prop := makeProperty(t, f.properties, hostID)
	b := makeBooking(t, guestID, prop.ID, booking.StatusCompleted)
	_ = f.bookings.Create(ctx, b)

	// A non-host (e.g. the guest) cannot review the guest.
	if _, err := f.svc.CreateGuestReview(ctx, reviewapp.GuestReviewInput{HostID: guestID, BookingID: b.ID, Rating: 5}); err != shared.ErrForbidden {
		t.Fatalf("non-host guest review err = %v, want ErrForbidden", err)
	}

	// The host can review the guest once.
	rv, err := f.svc.CreateGuestReview(ctx, reviewapp.GuestReviewInput{HostID: hostID, BookingID: b.ID, Rating: 5, Comment: "Great guest"})
	if err != nil {
		t.Fatalf("host guest review: %v", err)
	}
	if rv.GuestID != guestID || rv.AuthorID != hostID {
		t.Errorf("review subject/author mismatch: %+v", rv)
	}

	// Not twice.
	if _, err := f.svc.CreateGuestReview(ctx, reviewapp.GuestReviewInput{HostID: hostID, BookingID: b.ID, Rating: 3}); err == nil {
		t.Fatal("expected duplicate guest review to fail")
	}

	// The guest sees the review and a summary about themselves.
	about, err := f.svc.ListAboutGuest(ctx, guestID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list about guest: %v", err)
	}
	if about.Total != 1 {
		t.Fatalf("reviews about guest = %d, want 1", about.Total)
	}
	summary, _ := f.svc.SummaryForGuest(ctx, guestID)
	if summary.Count != 1 || summary.AverageRating != 5 {
		t.Errorf("guest summary = %+v, want count 1 avg 5", summary)
	}

	// A guest-to-property review on the same booking is still allowed (different direction).
	if _, err := f.svc.Create(ctx, reviewapp.CreateInput{GuestID: guestID, BookingID: b.ID, Rating: 4}); err != nil {
		t.Fatalf("property review after guest review should be allowed: %v", err)
	}
}
