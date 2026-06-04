package emailapp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	emailapp "github.com/airhost/backend/internal/application/email"
	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/email"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func TestEventHandler_SendsTransactionalEmails(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "host@test.dev", user.RoleHost)
	guest := mustUser(t, users, "guest@test.dev", user.RoleGuest)

	mailer := email.NewRecordingMailer()
	svc := emailapp.NewService(users, mailer)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	// A booking request emails the host; the confirmation emails the guest; a
	// message emails its recipient (the host here).
	dispatcher.Publish(ctx, event.BookingRequested{PropertyTitle: "Loft", HostID: host.ID, GuestID: guest.ID})
	dispatcher.Publish(ctx, event.BookingConfirmed{PropertyTitle: "Loft", GuestID: guest.ID})
	dispatcher.Publish(ctx, event.MessageSent{SenderID: guest.ID, RecipientID: host.ID})

	sent := mailer.Sent()
	if len(sent) != 3 {
		t.Fatalf("sent %d emails, want 3 (%+v)", len(sent), sent)
	}
	assertSent(t, sent, host.Email, "New booking request")
	assertSent(t, sent, guest.Email, "Booking confirmed")
	assertSent(t, sent, host.Email, "New message")

	// Each email carries a branded HTML body alongside the plaintext fallback.
	for _, m := range sent {
		if m.Text == "" {
			t.Fatalf("email to %s has empty text body", m.To)
		}
		if !strings.Contains(m.HTML, "<html") || !strings.Contains(m.HTML, "AirHost") {
			t.Fatalf("email to %s missing HTML layout: %q", m.To, m.HTML)
		}
	}
}

func TestEventHandler_HTMLEscapesUntrustedTitles(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "host@test.dev", user.RoleHost)

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	// A property title is user-controlled; the HTML body must not embed raw markup.
	dispatcher.Publish(ctx, event.BookingRequested{
		PropertyTitle: "<script>alert(1)</script>", HostID: host.ID,
	})

	sent := mailer.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(sent))
	}
	if strings.Contains(sent[0].HTML, "<script>") {
		t.Fatalf("HTML body contains unescaped markup: %q", sent[0].HTML)
	}
}

func TestEventHandler_CancellationEmailsTheOtherParty(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "host@test.dev", user.RoleHost)
	guest := mustUser(t, users, "guest@test.dev", user.RoleGuest)

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	// When the guest cancels, the host is the one emailed.
	dispatcher.Publish(ctx, event.BookingCancelled{
		PropertyTitle: "Loft", HostID: host.ID, GuestID: guest.ID, CancelledBy: guest.ID,
	})

	sent := mailer.Sent()
	if len(sent) != 1 || sent[0].To != host.Email || sent[0].Subject != "Booking cancelled" {
		t.Fatalf("cancellation email = %+v, want one to %s", sent, host.Email)
	}
}

func TestEventHandler_RespectsEmailOptOut(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "host@test.dev", user.RoleHost)
	// Opt the host out of booking emails but keep message emails.
	host.SetEmailPreferences(user.EmailPreferences{Bookings: false, Messages: true})
	if err := users.Update(ctx, host); err != nil {
		t.Fatalf("update prefs: %v", err)
	}

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	dispatcher.Publish(ctx, event.BookingRequested{PropertyTitle: "Loft", HostID: host.ID})
	dispatcher.Publish(ctx, event.MessageSent{RecipientID: host.ID})

	sent := mailer.Sent()
	if len(sent) != 1 || sent[0].Subject != "New message" {
		t.Fatalf("opt-out: sent = %+v, want only the new-message email", sent)
	}
}

func mustUser(t *testing.T, repo *memory.UserRepository, email string, role user.Role) *user.User {
	t.Helper()
	u, err := user.NewUser("sub-"+email, email, "Test User", role)
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

// S86 — ExperienceBooking events trigger transactional emails the same
// way property booking events do. The created event emails the host, the
// confirmed event emails the guest, and a cancel emails the OTHER party
// (whoever didn't trigger the cancel).
func TestEventHandler_ExperienceBookingEmails(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "exphost@test.dev", user.RoleHost)
	guest := mustUser(t, users, "expguest@test.dev", user.RoleGuest)

	mailer := email.NewRecordingMailer()
	svc := emailapp.NewService(users, mailer)
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCreated{
		BookingID: uuid.New(), ExperienceID: uuid.New(), ExperienceTitle: "Pasta workshop",
		HostID: host.ID, GuestID: guest.ID, TotalCents: 6600, Currency: "EUR",
	})
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingConfirmed{
		BookingID: uuid.New(), ExperienceID: uuid.New(), ExperienceTitle: "Pasta workshop",
		HostID: host.ID, GuestID: guest.ID,
	})
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCancelled{
		BookingID: uuid.New(), ExperienceID: uuid.New(), ExperienceTitle: "Pasta workshop",
		HostID: host.ID, GuestID: guest.ID, CancelledBy: host.ID,
	})

	sent := mailer.Sent()
	if len(sent) != 3 {
		t.Fatalf("sent %d emails, want 3 (%+v)", len(sent), sent)
	}
	assertSent(t, sent, host.Email, "New experience booking")
	assertSent(t, sent, guest.Email, "Experience booking confirmed")
	// host cancelled → guest is told
	assertSent(t, sent, guest.Email, "Experience booking cancelled")
}

func assertSent(t *testing.T, sent []email.Sent, to, subject string) {
	t.Helper()
	for _, m := range sent {
		if m.To == to && m.Subject == subject {
			return
		}
	}
	t.Fatalf("expected an email to %s with subject %q, got %+v", to, subject, sent)
}

// TestEventHandler_SplitPaymentCompletedEmails closes WF-GAP-011 on the
// email side: the organizer is emailed "Your trip is booked" and every
// other payer gets "Group trip booked" when their split fully funds.
func TestEventHandler_SplitPaymentCompletedEmails(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	organizer := mustUser(t, users, "alice@test.dev", user.RoleGuest)
	payer := mustUser(t, users, "bob@test.dev", user.RoleGuest)

	bookingRepo := memory.NewBookingRepository()
	splitRepo := memory.NewSplitPaymentRepository()
	mailer := email.NewRecordingMailer()
	svc := emailapp.NewService(users, mailer).
		WithBookings(bookingRepo).
		WithSplitPayments(splitRepo)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	bookingID := uuid.New()
	b := &booking.Booking{
		ID:         bookingID,
		PropertyID: uuid.New(),
		GuestID:    organizer.ID,
		Status:     booking.StatusConfirmed,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := bookingRepo.Create(ctx, b); err != nil {
		t.Fatalf("seed booking: %v", err)
	}

	splitID := uuid.New()
	now := time.Now().UTC()
	sp := &splitpayment.SplitPayment{
		ID:          splitID,
		BookingID:   bookingID,
		OrganizerID: organizer.ID,
		Currency:    "EUR",
		TotalCents:  10000,
		Status:      splitpayment.StatusCompleted,
		Shares: []splitpayment.Share{
			{ID: uuid.New(), SplitPaymentID: splitID, PayerEmail: organizer.Email, PayerUserID: &organizer.ID, AmountCents: 5000, Status: splitpayment.SharePaid, PaidAt: &now, CreatedAt: now, UpdatedAt: now},
			{ID: uuid.New(), SplitPaymentID: splitID, PayerEmail: payer.Email, PayerUserID: &payer.ID, AmountCents: 5000, Status: splitpayment.SharePaid, PaidAt: &now, CreatedAt: now, UpdatedAt: now},
		},
		CreatedAt:   now,
		UpdatedAt:   now,
		CompletedAt: &now,
	}
	if err := splitRepo.Create(ctx, sp); err != nil {
		t.Fatalf("seed split: %v", err)
	}

	dispatcher.Publish(ctx, event.SplitPaymentCompleted{SplitPaymentID: splitID, BookingID: bookingID})

	sent := mailer.Sent()
	if len(sent) != 2 {
		t.Fatalf("sent %d emails, want 2 (%+v)", len(sent), sent)
	}
	assertSent(t, sent, organizer.Email, "Your trip is booked")
	assertSent(t, sent, payer.Email, "Group trip booked")
}

// TestEventHandler_SplitPaymentCompletedRespectsOptOut confirms split-payment
// emails ride the catBookings channel — a user who muted booking emails sees
// nothing land in their inbox.
func TestEventHandler_SplitPaymentCompletedRespectsOptOut(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	organizer := mustUser(t, users, "alice@test.dev", user.RoleGuest)
	payer := mustUser(t, users, "bob@test.dev", user.RoleGuest)
	payer.SetEmailPreferences(user.EmailPreferences{Bookings: false, Messages: true})
	if err := users.Update(ctx, payer); err != nil {
		t.Fatalf("update prefs: %v", err)
	}

	bookingRepo := memory.NewBookingRepository()
	splitRepo := memory.NewSplitPaymentRepository()
	mailer := email.NewRecordingMailer()
	svc := emailapp.NewService(users, mailer).
		WithBookings(bookingRepo).
		WithSplitPayments(splitRepo)

	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(svc.EventHandler())

	bookingID := uuid.New()
	if err := bookingRepo.Create(ctx, &booking.Booking{
		ID: bookingID, PropertyID: uuid.New(), GuestID: organizer.ID,
		Status: booking.StatusConfirmed, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed booking: %v", err)
	}
	splitID := uuid.New()
	now := time.Now().UTC()
	if err := splitRepo.Create(ctx, &splitpayment.SplitPayment{
		ID: splitID, BookingID: bookingID, OrganizerID: organizer.ID,
		Currency: "EUR", TotalCents: 10000, Status: splitpayment.StatusCompleted,
		Shares: []splitpayment.Share{
			{ID: uuid.New(), SplitPaymentID: splitID, PayerEmail: organizer.Email, PayerUserID: &organizer.ID, AmountCents: 5000, Status: splitpayment.SharePaid, PaidAt: &now, CreatedAt: now, UpdatedAt: now},
			{ID: uuid.New(), SplitPaymentID: splitID, PayerEmail: payer.Email, PayerUserID: &payer.ID, AmountCents: 5000, Status: splitpayment.SharePaid, PaidAt: &now, CreatedAt: now, UpdatedAt: now},
		},
		CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}); err != nil {
		t.Fatalf("seed split: %v", err)
	}

	dispatcher.Publish(ctx, event.SplitPaymentCompleted{SplitPaymentID: splitID, BookingID: bookingID})

	sent := mailer.Sent()
	// Only the organizer (who didn't opt out) receives mail.
	if len(sent) != 1 || sent[0].To != organizer.Email {
		t.Fatalf("opt-out: sent = %+v, want only the organizer's email", sent)
	}
}
