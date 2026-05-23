package reviewapp_test

import (
	"context"
	"testing"
	"time"

	reviewapp "github.com/airhost/backend/internal/application/review"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func makeBooking(t *testing.T, guestID, propertyID uuid.UUID, status booking.Status) *booking.Booking {
	t.Helper()
	price, _ := shared.NewMoney(5000, "EUR")
	start := time.Now().UTC().AddDate(0, 0, 1)
	dr, err := booking.NewDateRange(start, start.AddDate(0, 0, 2))
	if err != nil {
		t.Fatalf("date range: %v", err)
	}
	b, err := booking.NewBooking(propertyID, guestID, dr, 1, price)
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

func setup(t *testing.T) (*reviewapp.Service, *memory.BookingRepository) {
	t.Helper()
	bookings := memory.NewBookingRepository()
	reviews := memory.NewReviewRepository()
	return reviewapp.NewService(reviews, bookings), bookings
}

func TestReview_RequiresCompletedBooking(t *testing.T) {
	svc, bookings := setup(t)
	guestID := uuid.New()
	b := makeBooking(t, guestID, uuid.New(), booking.StatusConfirmed)
	_ = bookings.Create(context.Background(), b)

	_, err := svc.Create(context.Background(), reviewapp.CreateInput{GuestID: guestID, BookingID: b.ID, Rating: 5})
	if err == nil {
		t.Fatal("expected review of non-completed booking to fail")
	}
}

func TestReview_OnlyGuestMayReview(t *testing.T) {
	svc, bookings := setup(t)
	guestID := uuid.New()
	b := makeBooking(t, guestID, uuid.New(), booking.StatusCompleted)
	_ = bookings.Create(context.Background(), b)

	_, err := svc.Create(context.Background(), reviewapp.CreateInput{GuestID: uuid.New(), BookingID: b.ID, Rating: 4})
	if err != shared.ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestReview_HappyPathThenNoDuplicates(t *testing.T) {
	svc, bookings := setup(t)
	guestID := uuid.New()
	b := makeBooking(t, guestID, uuid.New(), booking.StatusCompleted)
	_ = bookings.Create(context.Background(), b)

	rv, err := svc.Create(context.Background(), reviewapp.CreateInput{GuestID: guestID, BookingID: b.ID, Rating: 5, Comment: "Lovely"})
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	if rv.PropertyID != b.PropertyID {
		t.Error("review property id should match booking")
	}

	_, err = svc.Create(context.Background(), reviewapp.CreateInput{GuestID: guestID, BookingID: b.ID, Rating: 3})
	if err == nil {
		t.Fatal("expected duplicate review to fail")
	}

	summary, err := svc.Summary(context.Background(), b.PropertyID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.Count != 1 || summary.AverageRating != 5 {
		t.Errorf("summary = %+v, want count 1 avg 5", summary)
	}
}
