package emailapp_test

import (
	"context"
	"testing"

	emailapp "github.com/airhost/backend/internal/application/email"
	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/email"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
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

func assertSent(t *testing.T, sent []email.Sent, to, subject string) {
	t.Helper()
	for _, m := range sent {
		if m.To == to && m.Subject == subject {
			return
		}
	}
	t.Fatalf("expected an email to %s with subject %q, got %+v", to, subject, sent)
}
