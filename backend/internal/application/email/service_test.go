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
	"github.com/airhost/backend/internal/domain/property"
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

// TestEventHandler_BookingCancelled — S135. A single BookingCancelled event
// fans out TWO emails (mirrors the DisputeResolved pattern): the guest gets a
// guest-facing subject ("Your booking was cancelled — {title}") and the host
// gets a host-facing subject ("A guest cancelled their booking — {title}").
// When RefundFraction is non-zero the body mentions the refund percentage so
// the recipient can act without first opening the app. Rides catBookings so
// the existing booking opt-out preference applies on both sides.
func TestEventHandler_BookingCancelled(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "cancel-host@test.dev", user.RoleHost)
	guest := mustUser(t, users, "cancel-guest@test.dev", user.RoleGuest)

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	// Guest cancelled — both parties still get emailed (the dispute-outcome
	// pattern). A 50% refund is in flight.
	dispatcher.Publish(ctx, event.BookingCancelled{
		BookingID:      uuid.New(),
		PropertyID:     uuid.New(),
		PropertyTitle:  "Loft",
		HostID:         host.ID,
		GuestID:        guest.ID,
		CancelledBy:    guest.ID,
		RefundFraction: 0.5,
	})

	sent := mailer.Sent()
	if len(sent) != 2 {
		t.Fatalf("sent %d emails, want 2 (%+v)", len(sent), sent)
	}

	var guestMail, hostMail *email.Sent
	for i := range sent {
		switch sent[i].To {
		case guest.Email:
			guestMail = &sent[i]
		case host.Email:
			hostMail = &sent[i]
		}
	}
	if guestMail == nil {
		t.Fatalf("no email to guest (%s) in %+v", guest.Email, sent)
	}
	if guestMail.Subject != "Your booking was cancelled — Loft" {
		t.Fatalf("guest subject = %q, want %q", guestMail.Subject, "Your booking was cancelled — Loft")
	}
	if !strings.Contains(guestMail.Text, "Loft") {
		t.Fatalf("guest body should mention the property title, got %q", guestMail.Text)
	}
	if !strings.Contains(guestMail.Text, "50%") {
		t.Fatalf("guest body should mention the refund fraction, got %q", guestMail.Text)
	}

	if hostMail == nil {
		t.Fatalf("no email to host (%s) in %+v", host.Email, sent)
	}
	if hostMail.Subject != "A guest cancelled their booking — Loft" {
		t.Fatalf("host subject = %q, want %q", hostMail.Subject, "A guest cancelled their booking — Loft")
	}
	if !strings.Contains(hostMail.Text, "Loft") {
		t.Fatalf("host body should mention the property title, got %q", hostMail.Text)
	}
	if !strings.Contains(hostMail.Text, "50%") {
		t.Fatalf("host body should mention the refund fraction, got %q", hostMail.Text)
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
// (whoever didn't trigger the cancel). S127 refined the subject lines to
// embed the experience title; this test tracks the new convention.
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
	assertSent(t, sent, guest.Email, "Your experience is confirmed — Pasta workshop")
	// host cancelled → guest is told
	assertSent(t, sent, guest.Email, "Your experience booking was cancelled — Pasta workshop")
}

// TestEventHandler_ExperienceBookingEvents — S127. The email arm of the
// ExperienceBooking lifecycle (follow-on to S86): confirmed lands in the
// guest's inbox with the experience title in the subject + booking ref in
// the body; cancelled is routed to the OTHER party with the same conventions.
// Rides catExperiences so a future user-domain split between booking and
// experience opt-outs has a seam to plug into.
func TestEventHandler_ExperienceBookingEvents(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "s127-host@test.dev", user.RoleHost)
	guest := mustUser(t, users, "s127-guest@test.dev", user.RoleGuest)

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	confirmedID := uuid.New()
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingConfirmed{
		BookingID: confirmedID, ExperienceID: uuid.New(), ExperienceTitle: "Sunset kayak",
		HostID: host.ID, GuestID: guest.ID,
	})

	cancelledID := uuid.New()
	// Host cancels → the GUEST (the other party) gets the email.
	dispatcher.Publish(ctx, experiencebooking.ExperienceBookingCancelled{
		BookingID: cancelledID, ExperienceID: uuid.New(), ExperienceTitle: "Sunset kayak",
		HostID: host.ID, GuestID: guest.ID, CancelledBy: host.ID,
	})

	sent := mailer.Sent()
	if len(sent) != 2 {
		t.Fatalf("sent %d emails, want 2 (%+v)", len(sent), sent)
	}

	var confirm, cancel *email.Sent
	for i := range sent {
		switch sent[i].Subject {
		case "Your experience is confirmed — Sunset kayak":
			confirm = &sent[i]
		case "Your experience booking was cancelled — Sunset kayak":
			cancel = &sent[i]
		}
	}
	if confirm == nil {
		t.Fatalf("missing confirmed email in %+v", sent)
	}
	if confirm.To != guest.Email {
		t.Fatalf("confirm recipient = %q, want guest %q", confirm.To, guest.Email)
	}
	if !strings.Contains(confirm.Text, confirmedID.String()) {
		t.Fatalf("confirm body should embed the booking reference %s, got %q", confirmedID, confirm.Text)
	}
	if !strings.Contains(confirm.Text, "Sunset kayak") {
		t.Fatalf("confirm body should mention the experience title, got %q", confirm.Text)
	}

	if cancel == nil {
		t.Fatalf("missing cancelled email in %+v", sent)
	}
	if cancel.To != guest.Email {
		t.Fatalf("cancel recipient = %q, want the OTHER party (guest %q) since host cancelled", cancel.To, guest.Email)
	}
	if !strings.Contains(cancel.Text, cancelledID.String()) {
		t.Fatalf("cancel body should embed the booking reference %s, got %q", cancelledID, cancel.Text)
	}
	if !strings.Contains(cancel.Text, "Sunset kayak") {
		t.Fatalf("cancel body should mention the experience title, got %q", cancel.Text)
	}
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

// TestEventHandler_KYCStepUpRequired_SendsToGuestAccountCategory — S140 /
// WF-GAP-019 email arm. An unverified guest who trips the high-value gate
// gets a transactional account email naming the threshold amount (formatted
// as "{x.xx} {currency}") and the listing title so the message stands on
// its own without first opening the app. Single email, addressed to the
// guest, subject "Verify your identity to continue booking".
func TestEventHandler_KYCStepUpRequired_SendsToGuestAccountCategory(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	guest := mustUser(t, users, "kyc-guest@test.dev", user.RoleGuest)

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	dispatcher.Publish(ctx, event.KYCStepUpRequired{
		GuestID:        guest.ID,
		PropertyID:     uuid.New(),
		PropertyTitle:  "Sunny Flat",
		ThresholdCents: 50000,
		TotalCents:     75000,
		Currency:       "EUR",
	})

	sent := mailer.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 (%+v)", len(sent), sent)
	}
	if sent[0].To != guest.Email {
		t.Fatalf("recipient = %q, want guest %q", sent[0].To, guest.Email)
	}
	if !strings.Contains(sent[0].Subject, "Verify your identity") {
		t.Fatalf("subject = %q, want it to contain %q", sent[0].Subject, "Verify your identity")
	}
	if !strings.Contains(sent[0].Text, "Sunny Flat") {
		t.Fatalf("body should mention the property title, got %q", sent[0].Text)
	}
	// Threshold formatted as "{xx.xx} {Currency}" — 50000 cents in EUR.
	if !strings.Contains(sent[0].Text, "500.00 EUR") {
		t.Fatalf("body should mention the threshold amount %q, got %q", "500.00 EUR", sent[0].Text)
	}
}

// TestEventHandler_KYCStepUpRequired_NonOptOutCatAccount — the identity
// step-up nudge rides catAccount (account/security channel) and must land
// in the guest's inbox even when they've muted booking emails. Locks in
// the routing decision from S140: identity verification is security-
// adjacent, not opt-out-able, mirroring IdentityVerified and the
// Resolution Center notices.
func TestEventHandler_KYCStepUpRequired_NonOptOutCatAccount(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	guest := mustUser(t, users, "kyc-optout@test.dev", user.RoleGuest)
	// Mute everything that *is* opt-out-able. The KYC step-up arm rides
	// catAccount, which optedIn() always treats as opted-in.
	guest.SetEmailPreferences(user.EmailPreferences{Bookings: false, Messages: false})
	if err := users.Update(ctx, guest); err != nil {
		t.Fatalf("update prefs: %v", err)
	}

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	dispatcher.Publish(ctx, event.KYCStepUpRequired{
		GuestID:        guest.ID,
		PropertyID:     uuid.New(),
		PropertyTitle:  "Sunny Flat",
		ThresholdCents: 50000,
		TotalCents:     75000,
		Currency:       "EUR",
	})

	sent := mailer.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 even with Bookings opt-out (catAccount is non-opt-out); got %+v", len(sent), sent)
	}
	if sent[0].To != guest.Email {
		t.Fatalf("recipient = %q, want guest %q", sent[0].To, guest.Email)
	}
}

// TestEventHandler_KYCStepUpRequired_EmptyPropertyTitleFallsBack — defensive
// case: when the denormalised PropertyTitle on the event is empty (the
// emitter couldn't resolve the listing, or it raced a deletion) the body
// must still render cleanly via propertyTitleOr("", "a listing") rather
// than embedding a literal pair of empty quotes.
func TestEventHandler_KYCStepUpRequired_EmptyPropertyTitleFallsBack(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	guest := mustUser(t, users, "kyc-fallback@test.dev", user.RoleGuest)

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	dispatcher.Publish(ctx, event.KYCStepUpRequired{
		GuestID:        guest.ID,
		PropertyID:     uuid.New(),
		PropertyTitle:  "",
		ThresholdCents: 50000,
		TotalCents:     75000,
		Currency:       "EUR",
	})

	sent := mailer.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 (%+v)", len(sent), sent)
	}
	if !strings.Contains(sent[0].Text, "a listing") {
		t.Fatalf("body should fall back to %q when PropertyTitle is empty, got %q", "a listing", sent[0].Text)
	}
	// Belt-and-braces: the fallback must NOT leak an empty pair of quotes
	// into the rendered body.
	if strings.Contains(sent[0].Text, `""`) {
		t.Fatalf("body should not contain empty quotes, got %q", sent[0].Text)
	}
	// The threshold should still render, since the formatter doesn't care
	// about the title path.
	if !strings.Contains(sent[0].Text, "500.00 EUR") {
		t.Fatalf("body should still mention the threshold amount, got %q", sent[0].Text)
	}
}

// TestEventHandler_SplitShareEvents — S116 (follow-on to S88). The aggregate
// SplitPaymentCompleted event already triggers an organizer + payer fanout;
// this test covers the per-share arm: each payer should be told the moment
// their hold clears the gateway, and again if a later cancellation refunds
// that hold. Both rides the catBookings channel so booking opt-outs apply.
func TestEventHandler_SplitShareEvents(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	payer1 := mustUser(t, users, "payer1@test.dev", user.RoleGuest)
	payer2 := mustUser(t, users, "payer2@test.dev", user.RoleGuest)

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	dispatcher.Publish(ctx, event.SplitShareAuthorized{
		SplitPaymentID: uuid.New(),
		BookingID:      uuid.New(),
		ShareID:        uuid.New(),
		PayerID:        payer1.ID,
		AmountCents:    4250,
		Currency:       "EUR",
		GatewayRef:     "auth_abc123",
	})
	dispatcher.Publish(ctx, event.SplitShareRefunded{
		SplitPaymentID: uuid.New(),
		BookingID:      uuid.New(),
		ShareID:        uuid.New(),
		PayerID:        payer2.ID,
		AmountCents:    3300,
		Currency:       "USD",
		GatewayRef:     "ref_xyz789",
	})

	sent := mailer.Sent()
	if len(sent) != 2 {
		t.Fatalf("sent %d emails, want 2 (%+v)", len(sent), sent)
	}

	// Payer 1 — authorized hold.
	var auth, refund *email.Sent
	for i := range sent {
		switch sent[i].To {
		case payer1.Email:
			auth = &sent[i]
		case payer2.Email:
			refund = &sent[i]
		}
	}
	if auth == nil {
		t.Fatalf("no email to payer1 (%s) in %+v", payer1.Email, sent)
	}
	if auth.Subject != "Your share of a group booking is on hold" {
		t.Fatalf("payer1 subject = %q", auth.Subject)
	}
	if !strings.Contains(auth.Text, "42.50") || !strings.Contains(auth.Text, "EUR") {
		t.Fatalf("payer1 body should mention amount + currency, got %q", auth.Text)
	}

	// Payer 2 — refund notice.
	if refund == nil {
		t.Fatalf("no email to payer2 (%s) in %+v", payer2.Email, sent)
	}
	if refund.Subject != "Your share was refunded" {
		t.Fatalf("payer2 subject = %q", refund.Subject)
	}
	if !strings.Contains(refund.Text, "refunded") {
		t.Fatalf("payer2 body should mention the refund, got %q", refund.Text)
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

// TestEventHandler_DisputeOutcomeEvents — S131 (follow-on to the S13/S89
// Resolution Center wave). A moderator decision fires a single
// event.DisputeResolved whose Outcome flips between "resolved" (siding
// with the opener) and "rejected" (siding with the respondent); both
// parties should learn the outcome, and the subject line should reflect
// the disposition without leaking opener-vs-respondent personalisation
// (the event payload doesn't expose OpenerID, so the subject must stay
// symmetric across recipients). Rides catAccount: Resolution Center
// notices are account-level and can't be opted out of.
func TestEventHandler_DisputeOutcomeEvents(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "dispute-host@test.dev", user.RoleHost)
	guest := mustUser(t, users, "dispute-guest@test.dev", user.RoleGuest)

	// --- Resolved-in-favour case: both parties get the neutral "decision
	// has been made" subject; the resolution narrative is mirrored to both.
	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).EventHandler())

	dispatcher.Publish(ctx, event.DisputeResolved{
		DisputeID:     uuid.New(),
		BookingID:     uuid.New(),
		PropertyID:    uuid.New(),
		PropertyTitle: "Loft",
		HostID:        host.ID,
		GuestID:       guest.ID,
		Outcome:       "resolved",
		Resolution:    "Refunded one night for the broken AC.",
	})

	sent := mailer.Sent()
	if len(sent) != 2 {
		t.Fatalf("resolved: sent %d emails, want 2 (%+v)", len(sent), sent)
	}
	assertSent(t, sent, host.Email, "A decision has been made on your dispute")
	assertSent(t, sent, guest.Email, "A decision has been made on your dispute")
	// Body should embed the resolution text so the recipient can act
	// without first opening the app.
	for _, m := range sent {
		if !strings.Contains(m.Text, "Refunded one night") {
			t.Fatalf("resolved body should mention the resolution, got %q", m.Text)
		}
		if !strings.Contains(m.Text, "Loft") {
			t.Fatalf("resolved body should mention the property title, got %q", m.Text)
		}
	}

	// --- Rejected case: separate dispatcher so the recording mailer
	// starts empty and we can assert against just the two emails this
	// publish should produce.
	mailer2 := email.NewRecordingMailer()
	dispatcher2 := event.NewDispatcher()
	dispatcher2.Subscribe(emailapp.NewService(users, mailer2).EventHandler())

	dispatcher2.Publish(ctx, event.DisputeResolved{
		DisputeID:     uuid.New(),
		BookingID:     uuid.New(),
		PropertyID:    uuid.New(),
		PropertyTitle: "Loft",
		HostID:        host.ID,
		GuestID:       guest.ID,
		Outcome:       "rejected",
		Resolution:    "No evidence the AC was broken during the stay.",
	})

	sent2 := mailer2.Sent()
	if len(sent2) != 2 {
		t.Fatalf("rejected: sent %d emails, want 2 (%+v)", len(sent2), sent2)
	}
	assertSent(t, sent2, host.Email, "Your dispute was rejected")
	assertSent(t, sent2, guest.Email, "Your dispute was rejected")
}

// TestEventHandler_ReviewSubmitted_GuestToProperty_EmailsHost — S147 closes
// the dangling S136 subscriber. A guest-to-property ReviewSubmitted event
// must email the listing's host (resolved via the properties repo because
// the event doesn't snapshot HostID); subject "You got a new review", body
// names the property title and the rating (1-5 stars).
func TestEventHandler_ReviewSubmitted_GuestToProperty_EmailsHost(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "review-host@test.dev", user.RoleHost)
	guest := mustUser(t, users, "review-guest@test.dev", user.RoleGuest)

	properties := memory.NewPropertyRepository()
	propID := uuid.New()
	if err := properties.Create(ctx, &property.Property{
		ID:     propID,
		HostID: host.ID,
		Title:  "Sunny Loft",
	}); err != nil {
		t.Fatalf("seed property: %v", err)
	}

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).WithProperties(properties).EventHandler())

	dispatcher.Publish(ctx, event.ReviewSubmitted{
		ReviewID:   uuid.New(),
		BookingID:  uuid.New(),
		PropertyID: propID,
		AuthorID:   guest.ID,
		GuestID:    guest.ID,
		Direction:  "guest_to_property",
		Rating:     5,
	})

	sent := mailer.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 (%+v)", len(sent), sent)
	}
	if sent[0].To != host.Email {
		t.Fatalf("recipient = %q, want host %q", sent[0].To, host.Email)
	}
	if sent[0].Subject != "You got a new review" {
		t.Fatalf("subject = %q, want %q", sent[0].Subject, "You got a new review")
	}
	if !strings.Contains(sent[0].Text, "Sunny Loft") {
		t.Fatalf("body should mention the property title, got %q", sent[0].Text)
	}
	if !strings.Contains(sent[0].Text, "5/5") {
		t.Fatalf("body should mention the rating (5/5), got %q", sent[0].Text)
	}
}

// TestEventHandler_ReviewSubmitted_HostToGuest_EmailsGuest — host-on-guest
// reviews flip the recipient: the guest (whose id is already on the event)
// is the one told. Subject "Your host left you a review", body mentions
// the rating + a CTA pointing them at the app.
func TestEventHandler_ReviewSubmitted_HostToGuest_EmailsGuest(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	host := mustUser(t, users, "review-host2@test.dev", user.RoleHost)
	guest := mustUser(t, users, "review-guest2@test.dev", user.RoleGuest)

	properties := memory.NewPropertyRepository()
	propID := uuid.New()
	if err := properties.Create(ctx, &property.Property{
		ID:     propID,
		HostID: host.ID,
		Title:  "Beach Cottage",
	}); err != nil {
		t.Fatalf("seed property: %v", err)
	}

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).WithProperties(properties).EventHandler())

	dispatcher.Publish(ctx, event.ReviewSubmitted{
		ReviewID:   uuid.New(),
		BookingID:  uuid.New(),
		PropertyID: propID,
		AuthorID:   host.ID,
		GuestID:    guest.ID,
		Direction:  "host_to_guest",
		Rating:     4,
	})

	sent := mailer.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 (%+v)", len(sent), sent)
	}
	if sent[0].To != guest.Email {
		t.Fatalf("recipient = %q, want guest %q", sent[0].To, guest.Email)
	}
	if sent[0].Subject != "Your host left you a review" {
		t.Fatalf("subject = %q, want %q", sent[0].Subject, "Your host left you a review")
	}
	if !strings.Contains(sent[0].Text, "4/5") {
		t.Fatalf("body should mention the rating (4/5), got %q", sent[0].Text)
	}
}

// TestEventHandler_ReviewSubmitted_PropertyLookupFailsSilently — defensive
// case: the listing was deleted between the review write and the email
// dispatch (or any other repo error). The handler must log + short-circuit
// rather than panic or propagate; no email is sent.
func TestEventHandler_ReviewSubmitted_PropertyLookupFailsSilently(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	guest := mustUser(t, users, "review-orphan@test.dev", user.RoleGuest)

	// Empty repo — FindByID returns shared.ErrNotFound. The handler must
	// log and short-circuit; no panic and no email.
	properties := memory.NewPropertyRepository()

	mailer := email.NewRecordingMailer()
	dispatcher := event.NewDispatcher()
	dispatcher.Subscribe(emailapp.NewService(users, mailer).WithProperties(properties).EventHandler())

	dispatcher.Publish(ctx, event.ReviewSubmitted{
		ReviewID:   uuid.New(),
		BookingID:  uuid.New(),
		PropertyID: uuid.New(), // never seeded — lookup will miss
		AuthorID:   guest.ID,
		GuestID:    guest.ID,
		Direction:  "guest_to_property",
		Rating:     3,
	})

	if sent := mailer.Sent(); len(sent) != 0 {
		t.Fatalf("expected 0 emails when property lookup fails, got %d (%+v)", len(sent), sent)
	}
}
