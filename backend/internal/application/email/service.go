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
	"github.com/airhost/backend/internal/domain/experiencebooking"
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

// category groups emails so a recipient can opt out per kind.
type category int

const (
	catBookings category = iota
	catMessages
	catAccount // account/security emails — always delivered, not opt-out
)

// EventHandler returns an event.Handler that emails the relevant party. It is
// best-effort: failures are logged, never propagated to the publishing use case.
func (s *Service) EventHandler() event.Handler {
	return func(ctx context.Context, e event.Event) {
		switch ev := e.(type) {
		case event.BookingRequested:
			if ev.Instant {
				s.send(ctx, ev.HostID, catBookings, "New booking",
					fmt.Sprintf("A guest just booked %q (instant book). See it in your host dashboard.", ev.PropertyTitle))
			} else {
				s.send(ctx, ev.HostID, catBookings, "New booking request",
					fmt.Sprintf("A guest requested to book %q. Review it in your host dashboard.", ev.PropertyTitle))
			}

		case event.BookingConfirmed:
			s.send(ctx, ev.GuestID, catBookings, "Booking confirmed",
				fmt.Sprintf("Good news — your booking for %q is confirmed.", ev.PropertyTitle))

		case event.BookingCancelled:
			recipient := ev.GuestID
			if ev.CancelledBy == ev.GuestID {
				recipient = ev.HostID
			}
			s.send(ctx, recipient, catBookings, "Booking cancelled",
				fmt.Sprintf("A booking for %q was cancelled.", ev.PropertyTitle))

		case event.BookingCompleted:
			s.send(ctx, ev.GuestID, catBookings, "How was your stay?",
				fmt.Sprintf("Your stay at %q is complete. Leave a review to help other guests.", ev.PropertyTitle))
			s.send(ctx, ev.HostID, catBookings, "Review your guest",
				fmt.Sprintf("Your guest's stay at %q is complete. Leave a review of your guest.", ev.PropertyTitle))

		case event.MessageSent:
			s.send(ctx, ev.RecipientID, catMessages, "New message",
				"You have a new message on AirHost. Open the app to reply.")

		case event.IdentityVerified:
			s.send(ctx, ev.UserID, catAccount, "Identity verified",
				"Your identity has been verified. Your account now shows a verified badge.")

		case event.DisputeOpened:
			recipient := ev.HostID
			if ev.OpenerID == ev.HostID {
				recipient = ev.GuestID
			}
			s.send(ctx, recipient, catAccount, "Resolution Center case opened",
				fmt.Sprintf("A case was opened for the stay at %q. Sign in to add your side of the story.", ev.PropertyTitle))

		case event.DisputeResolved:
			body := fmt.Sprintf("A moderator %s the case on %q.\n\nDecision: %s", ev.Outcome, ev.PropertyTitle, ev.Resolution)
			s.send(ctx, ev.GuestID, catAccount, "Resolution Center decision", body)
			s.send(ctx, ev.HostID, catAccount, "Resolution Center decision", body)

		// Experience-booking lifecycle (S86). Same opt-out category as
		// property bookings so a guest who muted booking emails on web
		// doesn't suddenly start getting them from experiences.
		case experiencebooking.ExperienceBookingCreated:
			title := experienceTitleOr(ev.ExperienceTitle, "your experience")
			s.send(ctx, ev.HostID, catBookings, "New experience booking",
				fmt.Sprintf("A guest just booked %q. Review it in your host dashboard.", title))

		case experiencebooking.ExperienceBookingConfirmed:
			title := experienceTitleOr(ev.ExperienceTitle, "your experience")
			s.send(ctx, ev.GuestID, catBookings, "Experience booking confirmed",
				fmt.Sprintf("Good news — your booking for %q is confirmed.", title))

		case experiencebooking.ExperienceBookingCancelled:
			title := experienceTitleOr(ev.ExperienceTitle, "an experience")
			recipient := ev.GuestID
			if ev.CancelledBy == ev.GuestID {
				recipient = ev.HostID
			}
			s.send(ctx, recipient, catBookings, "Experience booking cancelled",
				fmt.Sprintf("A booking for %q was cancelled.", title))
		}
	}
}

// experienceTitleOr provides a generic fallback when the denormalised
// event title is empty (the service couldn't fetch the experience, or
// it was deleted between the booking write and the publish).
func experienceTitleOr(title, fallback string) string {
	if title == "" {
		return fallback
	}
	return title
}

func (s *Service) send(ctx context.Context, userID uuid.UUID, cat category, subject, body string) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		slog.Error("email: recipient lookup failed", "user", userID, "error", err)
		return
	}
	if u.Email == "" || !optedIn(u.EmailPrefs, cat) {
		return
	}
	msg := port.Email{To: u.Email, Subject: subject, Text: body, HTML: renderHTML(subject, body)}
	if err := s.mailer.Send(ctx, msg); err != nil {
		slog.Error("email: send failed", "to", u.Email, "subject", subject, "error", err)
	}
}

func optedIn(prefs user.EmailPreferences, cat category) bool {
	switch cat {
	case catAccount:
		return true // account/security emails cannot be opted out of
	case catMessages:
		return prefs.Messages
	default:
		return prefs.Bookings
	}
}
