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

// TestEventHandler_OfferEventsEmails closes the email arm of WF-GAP-008
// (S106). S99 already wired the in-app notification + realtime push for
// OfferCreated/Declined/Withdrawn; this asserts the matching transactional
// emails: Created → guest ("New offer", or "You're pre-approved" for the
// pre_approval kind), Declined → host, Withdrawn → guest.
func TestEventHandler_OfferEventsEmails(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "offerhost@test.dev", user.RoleHost)
	guest := mustUser(t, users, "offerguest@test.dev", user.RoleGuest)

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	dispatcher.Publish(ctx, event.OfferCreated{
		OfferID: uuid.New(), PropertyTitle: "Loft",
		HostID: host.ID, GuestID: guest.ID, Kind: "special_offer",
	})
	dispatcher.Publish(ctx, event.OfferDeclined{
		OfferID: uuid.New(), PropertyTitle: "Loft",
		HostID: host.ID, GuestID: guest.ID,
	})
	dispatcher.Publish(ctx, event.OfferWithdrawn{
		OfferID: uuid.New(), PropertyTitle: "Loft",
		HostID: host.ID, GuestID: guest.ID,
	})

	sent := mailer.Sent()
	if len(sent) != 3 {
		t.Fatalf("sent %d emails, want 3 (%+v)", len(sent), sent)
	}
	assertSent(t, sent, guest.Email, "New offer")
	assertSent(t, sent, host.Email, "Offer declined")
	assertSent(t, sent, guest.Email, "Offer withdrawn")
}

// TestEventHandler_OfferCreatedPreApprovalSubject verifies the special
// "You're pre-approved" subject for the pre_approval offer kind — the
// guest experience is "you can book now", not "review this offer".
func TestEventHandler_OfferCreatedPreApprovalSubject(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "prehost@test.dev", user.RoleHost)
	guest := mustUser(t, users, "preguest@test.dev", user.RoleGuest)

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	dispatcher.Publish(ctx, event.OfferCreated{
		OfferID: uuid.New(), PropertyTitle: "Loft",
		HostID: host.ID, GuestID: guest.ID, Kind: "pre_approval",
	})

	sent := mailer.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 (%+v)", len(sent), sent)
	}
	assertSent(t, sent, guest.Email, "You're pre-approved")
}

// TestEventHandler_CohostInvitedEmail closes the email arm of WF-GAP-016
// (S111 — follow-on to S99). S99 already wired the in-app notification +
// realtime push when a host grants someone cohost permissions on a
// listing; this asserts the matching transactional email lands in the
// invitee's inbox with the property title and at least one permission
// name in the body. The email rides catAccount (account-level role
// change, not opt-out).
func TestEventHandler_CohostInvitedEmail(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "cohost-host@test.dev", user.RoleHost)
	invitee := mustUser(t, users, "cohost-invitee@test.dev", user.RoleHost)

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	dispatcher.Publish(ctx, event.CohostInvited{
		CohostID:      uuid.New(),
		PropertyID:    uuid.New(),
		PropertyTitle: "Loft",
		HostID:        host.ID,
		UserID:        invitee.ID,
		Permissions:   []string{"manage_calendar", "reply_messages"},
	})

	sent := mailer.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 (%+v)", len(sent), sent)
	}
	if sent[0].To != invitee.Email {
		t.Fatalf("recipient = %q, want %q", sent[0].To, invitee.Email)
	}
	if sent[0].Subject != "You're now a co-host" {
		t.Fatalf("subject = %q, want %q", sent[0].Subject, "You're now a co-host")
	}
	if !strings.Contains(sent[0].Text, "Loft") {
		t.Fatalf("body should mention the property title, got %q", sent[0].Text)
	}
	if !strings.Contains(sent[0].Text, "manage_calendar") && !strings.Contains(sent[0].Text, "reply_messages") {
		t.Fatalf("body should mention at least one permission name, got %q", sent[0].Text)
	}
}

// TestSendArrivalInfoEmail — S107. Direct call (not event-driven) used by the
// arrival-info scheduler to mirror the in-app notification.
func TestSendArrivalInfoEmail(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	guest := mustUser(t, users, "guest@test.dev", user.RoleGuest)
	mailer := email.NewRecordingMailer()
	svc := emailapp.NewService(users, mailer)

	if err := svc.SendArrivalInfoEmail(ctx, guest.ID, "Atlantic Loft"); err != nil {
		t.Fatalf("SendArrivalInfoEmail: %v", err)
	}
	sent := mailer.Sent()
	if len(sent) != 1 {
		t.Fatalf("emails sent = %d, want 1", len(sent))
	}
	if sent[0].To != guest.Email || sent[0].Subject != "Check-in details available" {
		t.Fatalf("got %+v, want one to %s with subject 'Check-in details available'", sent[0], guest.Email)
	}
	if !strings.Contains(sent[0].Text, "Atlantic Loft") {
		t.Fatalf("body should mention the property title, got %q", sent[0].Text)
	}
}

// TestSendArrivalInfoEmail_RespectsBookingOptOut — a guest who muted booking
// emails doesn't get the arrival-info nudge.
func TestSendArrivalInfoEmail_RespectsBookingOptOut(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	guest := mustUser(t, users, "guest@test.dev", user.RoleGuest)
	guest.SetEmailPreferences(user.EmailPreferences{Bookings: false, Messages: true})
	if err := users.Update(ctx, guest); err != nil {
		t.Fatalf("update prefs: %v", err)
	}
	mailer := email.NewRecordingMailer()
	svc := emailapp.NewService(users, mailer)

	if err := svc.SendArrivalInfoEmail(ctx, guest.ID, "Atlantic Loft"); err != nil {
		t.Fatalf("SendArrivalInfoEmail: %v", err)
	}
	if len(mailer.Sent()) != 0 {
		t.Fatalf("opt-out: sent %d, want 0", len(mailer.Sent()))
	}
}
