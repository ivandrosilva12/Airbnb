package notificationapp

import (
	"context"
	"fmt"
	"github.com/airhost/backend/internal/observability/logctx"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/google/uuid"
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

		// Offer lifecycle (S99 — WF-GAP-008). Created → notify guest;
		// Declined → notify host; Withdrawn → notify guest. PushCatBookings
		// because the user thinks of these as part of their booking flow.
		case event.OfferCreated:
			title := propertyTitleOr(ev.PropertyTitle, "a property")
			body := fmt.Sprintf("A host sent you an offer for %q.", title)
			if ev.Kind == "pre_approval" {
				body = fmt.Sprintf("A host pre-approved you to book %q.", title)
			}
			err = s.create(ctx, ev.GuestID, notification.TypeOfferReceived,
				"New offer", body, ev.OfferID, PushCatBookings)
		case event.OfferDeclined:
			title := propertyTitleOr(ev.PropertyTitle, "your property")
			err = s.create(ctx, ev.HostID, notification.TypeOfferDeclined,
				"Offer declined",
				fmt.Sprintf("The guest declined your offer for %q.", title),
				ev.OfferID, PushCatBookings)
		case event.OfferWithdrawn:
			title := propertyTitleOr(ev.PropertyTitle, "a property")
			err = s.create(ctx, ev.GuestID, notification.TypeOfferWithdrawn,
				"Offer withdrawn",
				fmt.Sprintf("The host withdrew their offer for %q.", title),
				ev.OfferID, PushCatBookings)

		case event.CohostInvited:
			// S99 / WF-GAP-016 — the invitee is told they have a new
			// co-host grant. Account-category push because cohosting is a
			// role change, not a per-booking event.
			title := propertyTitleOr(ev.PropertyTitle, "a property")
			err = s.create(ctx, ev.UserID, notification.TypeCohostInvited,
				"You're now a co-host",
				fmt.Sprintf("A host added you as a co-host on %q. Check the cohost mailbox to start helping.", title),
				ev.PropertyID, PushCatAccount)

		case event.KYCStepUpRequired:
			// S140 / WF-GAP-019 — the guest hit the high-value gate
			// without a verified identity. Account-category push because
			// the prompt is to verify identity, not to act on a booking.
			// The bookingapp.Service dedupes emissions per (guest, listing,
			// currency) TTL so a guest retrying the same booking doesn't
			// spawn duplicate prompts. PropertyID is the resource the user
			// taps from the notification to deep-link back to the listing.
			title := propertyTitleOr(ev.PropertyTitle, "a property")
			err = s.create(ctx, ev.GuestID, notification.TypeKYCStepUpRequired,
				"Verify your identity to continue",
				fmt.Sprintf("Bookings for %q at this amount need a verified identity. Verify to retry.", title),
				ev.PropertyID, PushCatAccount)

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

		case event.ReviewSubmitted:
			// S148 — the in-app arm of S136's ReviewSubmitted outbox
			// event. guest_to_property → host of the listing learns a
			// guest reviewed them; host_to_guest → guest learns their
			// host reviewed them. The host of a property review isn't
			// on the event payload (kept lean) so we look it up via
			// the optional property repo; if the listing was deleted
			// between emit and consume we log and short-circuit
			// rather than ship a notification with no addressee. The
			// host_to_guest branch doesn't need any lookup — the
			// recipient is GuestID.
			switch ev.Direction {
			case "guest_to_property":
				if s.properties == nil {
					logctx.LoggerFrom(ctx).Error("review-submitted: properties repo not wired", "event", e.EventName(), "review", ev.ReviewID)
					break
				}
				p, e1 := s.properties.FindByID(ctx, ev.PropertyID)
				if e1 != nil {
					logctx.LoggerFrom(ctx).Error("review-submitted: property lookup failed", "event", e.EventName(), "property", ev.PropertyID, "error", e1)
					break
				}
				body := "A guest reviewed your listing."
				if p.Title != "" {
					body = fmt.Sprintf("A guest reviewed %q.", p.Title)
				}
				err = s.create(ctx, p.HostID, notification.TypeReviewSubmitted,
					"New review", body, ev.ReviewID, PushCatBookings)
			case "host_to_guest":
				err = s.create(ctx, ev.GuestID, notification.TypeReviewSubmitted,
					"You have a new review",
					"Your host left you a review on your recent stay.",
					ev.ReviewID, PushCatBookings)
			}

		case event.PaymentCaptured:
			// S157 — closes the dangling PaymentCaptured event (S123 era).
			// Payment is real now: the gateway took the money, so the
			// guest deserves a heads-up before the line item lands on
			// their card statement. PushCatBookings because the guest
			// thinks of the charge as part of the booking flow, not an
			// account-management notice. RelatedID is the booking so
			// tapping the prompt deep-links back to the stay it paid for.
			amount := fmt.Sprintf("%.2f %s", float64(ev.AmountCents)/100, ev.Currency)
			err = s.create(ctx, ev.GuestID, notification.TypePaymentCaptured,
				"Payment received",
				fmt.Sprintf("Your card was charged %s for booking %s.", amount, ev.BookingID),
				ev.BookingID, PushCatBookings)

		case event.PaymentRefunded:
			// S157 — closes the dangling PaymentRefunded event (S123 era).
			// Sibling of PaymentCaptured: when a cancellation (or any
			// other refund path) releases captured funds the guest is
			// told the credit is on its way so they can reconcile the
			// movement on their card statement. Same channel + related
			// resource as the capture arm.
			amount := fmt.Sprintf("%.2f %s", float64(ev.RefundedCents)/100, ev.Currency)
			err = s.create(ctx, ev.GuestID, notification.TypePaymentRefunded,
				"Refund issued",
				fmt.Sprintf("A refund of %s was sent back to your card for booking %s.", amount, ev.BookingID),
				ev.BookingID, PushCatBookings)

		case event.SplitPaymentCompleted:
			// S93 / WF-GAP-011. Every share is now authorised, so the
			// booking confirms. Notify the organizer (resolved from the
			// booking aggregate, which is where the authoritative "who
			// owns this reservation" lives) and every other payer who
			// pitched in. Repo dependencies are optional — if the wiring
			// hasn't supplied them (older tests), silently skip.
			if s.bookings == nil || s.splits == nil {
				break
			}
			b, e1 := s.bookings.FindByID(ctx, ev.BookingID)
			if e1 != nil {
				logctx.LoggerFrom(ctx).Error("split-completed: booking lookup failed", "event", e.EventName(), "booking", ev.BookingID, "error", e1)
				break
			}
			sp, e1 := s.splits.FindByID(ctx, ev.SplitPaymentID)
			if e1 != nil {
				logctx.LoggerFrom(ctx).Error("split-completed: split lookup failed", "event", e.EventName(), "split", ev.SplitPaymentID, "error", e1)
				break
			}
			// Organizer (booking.GuestID) gets the headline message.
			if e1 := s.create(ctx, b.GuestID, notification.TypeSplitPaymentCompleted,
				"Your trip is booked",
				"Everyone paid their share — your reservation is confirmed.",
				ev.BookingID, PushCatBookings); e1 != nil {
				logctx.LoggerFrom(ctx).Error("failed to create notification", "event", e.EventName(), "error", e1)
			}
			// Bonus: notify every other payer who authorised a share.
			// Dedup against the organizer (whose share is also in the list)
			// so they don't receive two notifications.
			seen := map[uuid.UUID]struct{}{b.GuestID: {}}
			for _, share := range sp.Shares {
				if share.PayerUserID == nil {
					continue
				}
				payerID := *share.PayerUserID
				if _, dup := seen[payerID]; dup {
					continue
				}
				seen[payerID] = struct{}{}
				if e1 := s.create(ctx, payerID, notification.TypeSplitPaymentCompleted,
					"Group trip booked",
					"The split-payment plan you contributed to is now fully funded — the trip is confirmed.",
					ev.BookingID, PushCatBookings); e1 != nil {
					logctx.LoggerFrom(ctx).Error("failed to create notification", "event", e.EventName(), "error", e1)
				}
			}
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

// propertyTitleOr is the same fallback dance for property-titled events
// (offers, in particular — the host may have deleted the listing between
// dispatching and consuming the event).
func propertyTitleOr(title, fallback string) string {
	if title == "" {
		return fallback
	}
	return title
}
