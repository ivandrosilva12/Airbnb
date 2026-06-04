package arrivalapp_test

import (
	"context"
	"testing"
	"time"

	arrivalapp "github.com/airhost/backend/internal/application/arrival"
	notificationapp "github.com/airhost/backend/internal/application/notification"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// pinClock returns a function that always reports the same instant.
func pinClock(now time.Time) func() time.Time { return func() time.Time { return now } }

func TestService_NotifiesGuestsInsideRevealWindow(t *testing.T) {
	ctx := context.Background()
	bookings := memory.NewBookingRepository()
	properties := memory.NewPropertyRepository()
	notif := notificationapp.NewService(memory.NewNotificationRepository())

	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	guest1 := uuid.New()
	guest2 := uuid.New()
	guestFar := uuid.New()
	hostID := uuid.New()

	prop := newSeedProperty(t, properties, hostID, "Atlantic Loft")

	// In window: check-in 24h from now (well inside the 48h reveal).
	mustCreateBooking(t, bookings, prop.ID, guest1, now.Add(24*time.Hour), now.Add(3*24*time.Hour))
	// At edge: check-in 47h from now — still inside.
	mustCreateBooking(t, bookings, prop.ID, guest2, now.Add(47*time.Hour), now.Add(5*24*time.Hour))
	// Out of window: check-in 5 days from now — too early for the reveal.
	mustCreateBooking(t, bookings, prop.ID, guestFar, now.Add(5*24*time.Hour), now.Add(8*24*time.Hour))

	svc := arrivalapp.NewService(bookings, properties, notif).WithClock(pinClock(now))

	sent, err := svc.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sent != 2 {
		t.Fatalf("notifications sent = %d, want 2", sent)
	}
	if c, _ := notif.UnreadCount(ctx, guest1); c != 1 {
		t.Errorf("guest1 unread = %d, want 1", c)
	}
	if c, _ := notif.UnreadCount(ctx, guest2); c != 1 {
		t.Errorf("guest2 unread = %d, want 1", c)
	}
	if c, _ := notif.UnreadCount(ctx, guestFar); c != 0 {
		t.Errorf("guestFar unread = %d, want 0 (outside window)", c)
	}
}

func TestService_IdempotentAcrossTicks(t *testing.T) {
	ctx := context.Background()
	bookings := memory.NewBookingRepository()
	properties := memory.NewPropertyRepository()
	notif := notificationapp.NewService(memory.NewNotificationRepository())

	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	hostID := uuid.New()
	guest := uuid.New()
	prop := newSeedProperty(t, properties, hostID, "Atlantic Loft")
	mustCreateBooking(t, bookings, prop.ID, guest, now.Add(20*time.Hour), now.Add(3*24*time.Hour))

	svc := arrivalapp.NewService(bookings, properties, notif).WithClock(pinClock(now))

	first, _ := svc.Run(ctx)
	if first != 1 {
		t.Fatalf("first run sent %d, want 1", first)
	}
	// Re-run a few minutes later — the guest still sits inside the window
	// but should NOT get a second notification.
	svc.WithClock(pinClock(now.Add(5 * time.Minute)))
	second, _ := svc.Run(ctx)
	if second != 0 {
		t.Fatalf("second run sent %d, want 0 (dedupe)", second)
	}
	if c, _ := notif.UnreadCount(ctx, guest); c != 1 {
		t.Fatalf("guest unread = %d, want 1 across two ticks", c)
	}
}

func TestService_SkipsCancelledAndPending(t *testing.T) {
	ctx := context.Background()
	bookings := memory.NewBookingRepository()
	properties := memory.NewPropertyRepository()
	notif := notificationapp.NewService(memory.NewNotificationRepository())

	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	hostID := uuid.New()
	guestConfirmed := uuid.New()
	guestPending := uuid.New()
	guestCancelled := uuid.New()
	prop := newSeedProperty(t, properties, hostID, "Atlantic Loft")

	mustCreateBooking(t, bookings, prop.ID, guestConfirmed, now.Add(12*time.Hour), now.Add(3*24*time.Hour))
	// Pending booking — same dates, but status pending: should be skipped.
	pending := mustCreateBooking(t, bookings, prop.ID, guestPending, now.Add(12*time.Hour), now.Add(3*24*time.Hour))
	pending.Status = booking.StatusPending
	if err := bookings.Update(ctx, pending); err != nil {
		t.Fatalf("update pending: %v", err)
	}
	// Cancelled booking — also skipped.
	cancelled := mustCreateBooking(t, bookings, prop.ID, guestCancelled, now.Add(12*time.Hour), now.Add(3*24*time.Hour))
	cancelled.Status = booking.StatusCancelled
	if err := bookings.Update(ctx, cancelled); err != nil {
		t.Fatalf("update cancelled: %v", err)
	}

	svc := arrivalapp.NewService(bookings, properties, notif).WithClock(pinClock(now))
	sent, err := svc.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sent != 1 {
		t.Fatalf("sent = %d, want 1 (only confirmed)", sent)
	}
	if c, _ := notif.UnreadCount(ctx, guestConfirmed); c != 1 {
		t.Errorf("confirmed guest unread = %d, want 1", c)
	}
	if c, _ := notif.UnreadCount(ctx, guestPending); c != 0 {
		t.Errorf("pending guest unread = %d, want 0", c)
	}
	if c, _ := notif.UnreadCount(ctx, guestCancelled); c != 0 {
		t.Errorf("cancelled guest unread = %d, want 0", c)
	}
}

// --- helpers ---------------------------------------------------------------

func newSeedProperty(t *testing.T, repo property.Repository, hostID uuid.UUID, title string) *property.Property {
	t.Helper()
	price, _ := shared.NewMoney(6000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{
		Line1: "Rua 1", City: "Lisbon", Country: "PT",
		Latitude: 38.7, Longitude: -9.1,
	}
	p, err := property.NewProperty(hostID, title, "desc", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("create property: %v", err)
	}
	return p
}

func mustCreateBooking(t *testing.T, repo booking.Repository, propertyID, guestID uuid.UUID, checkIn, checkOut time.Time) *booking.Booking {
	t.Helper()
	dates, err := booking.NewDateRange(checkIn, checkOut)
	if err != nil {
		t.Fatalf("new date range: %v", err)
	}
	b := &booking.Booking{
		ID:         uuid.New(),
		PropertyID: propertyID,
		GuestID:    guestID,
		Dates:      dates,
		Guests:     1,
		Status:     booking.StatusConfirmed,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), b); err != nil {
		t.Fatalf("create booking: %v", err)
	}
	return b
}

// fakeEmailer records the calls the arrival scheduler makes to the emailer
// port (S107). Used to assert the scheduler mirrors each in-app notification
// to a transactional email when an emailer is wired.
type fakeEmailer struct {
	calls []struct {
		guestID uuid.UUID
		title   string
	}
}

func (f *fakeEmailer) SendArrivalInfoEmail(_ context.Context, guestID uuid.UUID, title string) error {
	f.calls = append(f.calls, struct {
		guestID uuid.UUID
		title   string
	}{guestID, title})
	return nil
}

// TestService_EmailMirrorsNotificationsWhenWired — S107. The Emailer is
// optional; when set, the scheduler emails every guest it also notified.
func TestService_EmailMirrorsNotificationsWhenWired(t *testing.T) {
	ctx := context.Background()
	bookings := memory.NewBookingRepository()
	properties := memory.NewPropertyRepository()
	notif := notificationapp.NewService(memory.NewNotificationRepository())
	emailer := &fakeEmailer{}

	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	hostID := uuid.New()
	guest1 := uuid.New()
	guest2 := uuid.New()
	prop := newSeedProperty(t, properties, hostID, "Atlantic Loft")
	mustCreateBooking(t, bookings, prop.ID, guest1, now.Add(20*time.Hour), now.Add(3*24*time.Hour))
	mustCreateBooking(t, bookings, prop.ID, guest2, now.Add(40*time.Hour), now.Add(5*24*time.Hour))

	svc := arrivalapp.NewService(bookings, properties, notif).
		WithEmailer(emailer).
		WithClock(pinClock(now))

	sent, err := svc.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sent != 2 {
		t.Fatalf("notifications = %d, want 2", sent)
	}
	if len(emailer.calls) != 2 {
		t.Fatalf("emailer calls = %d, want 2 (one per notified guest)", len(emailer.calls))
	}
	// Each call must carry the property title so the email body isn't generic.
	for i, c := range emailer.calls {
		if c.title != "Atlantic Loft" {
			t.Errorf("emailer call[%d] title = %q, want %q", i, c.title, "Atlantic Loft")
		}
	}
}

// TestService_NoEmailerStillNotifies — WithEmailer is optional; pre-S107
// harnesses that don't wire one still work.
func TestService_NoEmailerStillNotifies(t *testing.T) {
	ctx := context.Background()
	bookings := memory.NewBookingRepository()
	properties := memory.NewPropertyRepository()
	notif := notificationapp.NewService(memory.NewNotificationRepository())

	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	hostID := uuid.New()
	guest := uuid.New()
	prop := newSeedProperty(t, properties, hostID, "Atlantic Loft")
	mustCreateBooking(t, bookings, prop.ID, guest, now.Add(20*time.Hour), now.Add(3*24*time.Hour))

	svc := arrivalapp.NewService(bookings, properties, notif).WithClock(pinClock(now))
	sent, err := svc.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sent != 1 {
		t.Fatalf("notifications = %d, want 1", sent)
	}
}
