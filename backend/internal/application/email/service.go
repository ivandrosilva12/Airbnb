// Package emailapp turns domain events into transactional emails. It reacts to
// the same events as in-app notifications but delivers via a Mailer port,
// resolving recipient addresses from the user repository.
package emailapp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// Service sends event-driven emails.
type Service struct {
	users  user.Repository
	mailer port.Mailer
}

// NewService wires the email application service.
func NewService(users user.Repository, mailer port.Mailer) *Service {
	return &Service{users: users, mailer: mailer}
}

// EventHandler returns an event.Handler that emails the relevant party. It is
// best-effort: failures are logged, never propagated to the publishing use case.
func (s *Service) EventHandler() event.Handler {
	return func(ctx context.Context, e event.Event) {
		switch ev := e.(type) {
		case event.BookingRequested:
			s.send(ctx, ev.HostID, "New booking request",
				fmt.Sprintf("A guest requested to book %q. Review it in your host dashboard.", ev.PropertyTitle))

		case event.BookingConfirmed:
			s.send(ctx, ev.GuestID, "Booking confirmed",
				fmt.Sprintf("Good news — your booking for %q is confirmed.", ev.PropertyTitle))

		case event.BookingCancelled:
			recipient := ev.GuestID
			if ev.CancelledBy == ev.GuestID {
				recipient = ev.HostID
			}
			s.send(ctx, recipient, "Booking cancelled",
				fmt.Sprintf("A booking for %q was cancelled.", ev.PropertyTitle))

		case event.MessageSent:
			s.send(ctx, ev.RecipientID, "New message",
				"You have a new message on AirHost. Open the app to reply.")
		}
	}
}

func (s *Service) send(ctx context.Context, userID uuid.UUID, subject, body string) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		slog.Error("email: recipient lookup failed", "user", userID, "error", err)
		return
	}
	if u.Email == "" {
		return
	}
	msg := port.Email{To: u.Email, Subject: subject, Text: body, HTML: renderHTML(subject, body)}
	if err := s.mailer.Send(ctx, msg); err != nil {
		slog.Error("email: send failed", "to", u.Email, "subject", subject, "error", err)
	}
}
