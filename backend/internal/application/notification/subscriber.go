package notificationapp

import (
	"context"
	"fmt"
	"github.com/airhost/backend/internal/observability/logctx"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/experiencebooking"
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
			err = s.create(ctx, ev.HostID, notification.TypeBookingRequested, title, body, ev.BookingID, PushCatBookings)

		case event.BookingConfirmed:
			err = s.create(ctx, ev.GuestID, notification.TypeBookingConfirmed,
				"Booking confirmed",
				fmt.Sprintf("Your booking for %q was confirmed.", ev.PropertyTitle), ev.BookingID, PushCatBookings)

		case event.BookingModified:
			err = s.create(ctx, ev.HostID, notification.TypeBookingModified,
				"Booking changed",
				fmt.Sprintf("A guest changed their booking dates or party size for %q.", ev.PropertyTitle), ev.BookingID, PushCatBookings)

		case event.BookingCancelled:
			recipient := ev.GuestID
			if ev.CancelledBy == ev.GuestID {
				recipient = ev.HostID
			}
			err = s.create(ctx, recipient, notification.TypeBookingCancelled,
				"Booking cancelled",
				fmt.Sprintf("A booking for %q was cancelled.", ev.PropertyTitle), ev.BookingID, PushCatBookings)

		case event.BookingCompleted:
			// Prompt the guest to review the property, and the host to review the
			// guest. The guest prompt's failure is logged separately so the host
			// prompt still runs.
			if e1 := s.create(ctx, ev.GuestID, notification.TypeReviewRequested,
				"How was your stay?",
				fmt.Sprintf("Leave a review for %q.", ev.PropertyTitle), ev.BookingID, PushCatBookings); e1 != nil {
				logctx.LoggerFrom(ctx).Error("failed to create notification", "event", e.EventName(), "error", e1)
			}
			err = s.create(ctx, ev.HostID, notification.TypeReviewRequested,
				"Review your guest",
				fmt.Sprintf("Leave a review for your guest at %q.", ev.PropertyTitle), ev.BookingID, PushCatBookings)

		case event.MessageSent:
			err = s.create(ctx, ev.RecipientID, notification.TypeMessageReceived,
				"New message",
				"You have a new message.", ev.ConversationID, PushCatMessages)

		case event.IdentityVerified:
			err = s.create(ctx, ev.UserID, notification.TypeIdentityVerified,
				"Identity verified",
				"Your identity has been verified. You now have a verified badge.", ev.VerificationID, PushCatAccount)

		case event.DisputeOpened:
			// The non-opener party is notified so they can respond. We always notify
			// the opposite side; admins see the case in the moderation queue.
			recipient := ev.HostID
			if ev.OpenerID == ev.HostID {
				recipient = ev.GuestID
			}
			err = s.create(ctx, recipient, notification.TypeDisputeOpened,
				"Resolution Center case opened",
				fmt.Sprintf("A case was opened on %q. Open it to add your side.", ev.PropertyTitle), ev.DisputeID, PushCatAccount)

		case event.DisputeResolved:
			// Both parties are informed of the outcome.
			title := "Resolution Center decision"
			body := fmt.Sprintf("A moderator %s the case on %q.", ev.Outcome, ev.PropertyTitle)
			if e1 := s.create(ctx, ev.GuestID, notification.TypeDisputeResolved, title, body, ev.DisputeID, PushCatAccount); e1 != nil {
				logctx.LoggerFrom(ctx).Error("failed to create notification", "event", e.EventName(), "error", e1)
			}
			err = s.create(ctx, ev.HostID, notification.TypeDisputeResolved, title, body, ev.DisputeID, PushCatAccount)

		// Experience-booking lifecycle (S86) — mirrors the property-booking
		// fan-out: host learns about the request, guest learns about the
		// confirmation, the OTHER party learns about a cancellation.
		case experiencebooking.ExperienceBookingCreated:
			title := experienceTitleOr(ev.ExperienceTitle, "your experience")
			err = s.create(ctx, ev.HostID, notification.TypeExperienceBookingRequested,
				"New experience booking",
				fmt.Sprintf("A guest just booked %q.", title), ev.BookingID, PushCatBookings)

		case experiencebooking.ExperienceBookingConfirmed:
			title := experienceTitleOr(ev.ExperienceTitle, "your experience")
			err = s.create(ctx, ev.GuestID, notification.TypeExperienceBookingConfirmed,
				"Experience booking confirmed",
				fmt.Sprintf("Your booking for %q was confirmed.", title), ev.BookingID, PushCatBookings)

		case experiencebooking.ExperienceBookingCancelled:
			title := experienceTitleOr(ev.ExperienceTitle, "an experience")
			// The canceller doesn't need to be told what they just did —
			// the OTHER party does. CancelledBy is the actor.
			recipient := ev.GuestID
			if ev.CancelledBy == ev.GuestID {
				recipient = ev.HostID
			}
			err = s.create(ctx, recipient, notification.TypeExperienceBookingCancelled,
				"Experience booking cancelled",
				fmt.Sprintf("A booking for %q was cancelled.", title), ev.BookingID, PushCatBookings)
		}
		if err != nil {
			logctx.LoggerFrom(ctx).Error("failed to create notification", "event", e.EventName(), "error", err)
		}
	}
}

// experienceTitleOr returns the experience title from the event when set,
// or a generic fallback. Belt-and-braces: lookupTitle in the service may
// have failed (deleted experience, transient repo error) and we'd rather
// say "your experience" than render an empty pair of quotes.
func experienceTitleOr(title, fallback string) string {
	if title == "" {
		return fallback
	}
	return title
}
