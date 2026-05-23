package reviewapp_test

import (
	"context"
	"testing"
	"time"

	reviewapp "github.com/airhost/backend/internal/application/review"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

type reviewFixture struct {
	svc        *reviewapp.Service
	bookings   *memory.BookingRepository
	properties *memory.PropertyRepository
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
	b, err := booking.NewBooking(propertyID, guestID, dr, 1, price, cleaning, 0)
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
