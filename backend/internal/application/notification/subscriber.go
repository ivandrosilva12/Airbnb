package notificationapp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/notification"
)

// EventHandler returns an event.Handler that creates notifications in reaction
// to domain events. Register it with the dispatcher. Failures are logged but
// never propagated — notifications are best-effort.
func (s *Service) EventHandler() event.Handler {
	return func(ctx context.Context, e event.Event) {
		var err error
		switch ev := e.(type) {
		case event.BookingRequested:
			title, body := "New booking request", fmt.Sprintf("A guest requested to book %q.", ev.PropertyTitle)
			if ev.Instant {
				title, body = "New booking", fmt.Sprintf("A guest just booked %q (instant book).", ev.PropertyTitle)
			}
			err = s.create(ctx, ev.HostID, notification.TypeBookingRequested, title, body, ev.BookingID)

		case event.BookingConfirmed:
			err = s.create(ctx, ev.GuestID, notification.TypeBookingConfirmed,
				"Booking confirmed",
				fmt.Sprintf("Your booking for %q was confirmed.", ev.PropertyTitle), ev.BookingID)

		case event.BookingCancelled:
			recipient := ev.GuestID
			if ev.CancelledBy == ev.GuestID {
				recipient = ev.HostID
			}
			err = s.create(ctx, recipient, notification.TypeBookingCancelled,
				"Booking cancelled",
				fmt.Sprintf("A booking for %q was cancelled.", ev.PropertyTitle), ev.BookingID)

		case event.BookingCompleted:
			// Prompt the guest to review the property, and the host to review the
			// guest. The guest prompt's failure is logged separately so the host
			// prompt still runs.
			if e1 := s.create(ctx, ev.GuestID, notification.TypeReviewRequested,
				"How was your stay?",
				fmt.Sprintf("Leave a review for %q.", ev.PropertyTitle), ev.BookingID); e1 != nil {
				slog.Error("failed to create notification", "event", e.EventName(), "error", e1)
			}
			err = s.create(ctx, ev.HostID, notification.TypeReviewRequested,
				"Review your guest",
				fmt.Sprintf("Leave a review for your guest at %q.", ev.PropertyTitle), ev.BookingID)

		case event.MessageSent:
			err = s.create(ctx, ev.RecipientID, notification.TypeMessageReceived,
				"New message",
				"You have a new message.", ev.ConversationID)

		case event.IdentityVerified:
			err = s.create(ctx, ev.UserID, notification.TypeIdentityVerified,
				"Identity verified",
				"Your identity has been verified. You now have a verified badge.", ev.VerificationID)
		}
		if err != nil {
			slog.Error("failed to create notification", "event", e.EventName(), "error", err)
		}
	}
}
