// Package emailapp turns domain events into transactional emails. It reacts to
// the same events as in-app notifications but delivers via a Mailer port,
// resolving recipient addresses from the user repository.
package emailapp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// Service sends event-driven emails.
type Service struct {
	users    user.Repository
	mailer   port.Mailer
	bookings booking.Repository      // optional — only used by the split-payment subscriber
	splits   splitpayment.Repository // optional — only used by the split-payment subscriber
}

// NewService wires the email application service.
func NewService(users user.Repository, mailer port.Mailer) *Service {
	return &Service{users: users, mailer: mailer}
}

// WithBookings attaches a booking repository so the event subscriber can
// resolve organizer/guest identities on split-payment events. Optional — the
// matching handler short-circuits when the dep is nil.
func (s *Service) WithBookings(r booking.Repository) *Service {
	s.bookings = r
	return s
}

// WithSplitPayments attaches a split-payment repository so the email
// subscriber can fan out a "trip is booked" notice to every payer when a
// split completes. Optional — the handler short-circuits when nil.
func (s *Service) WithSplitPayments(r splitpayment.Repository) *Service {
	s.splits = r
	return s
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

		case event.SplitPaymentCompleted:
			// S93 / WF-GAP-011 — fan email out to organizer + payers.
			s.handleSplitCompleted(ctx, ev)

		// Per-share split-payment lifecycle (S116 — follow-on to S88).
		// SplitPaymentCompleted (above) only fires once everyone has paid;
		// individual payers also deserve a confirmation the moment their
		// share clears the gateway, and a refund notice if a cancellation
		// later releases that hold. Same opt-out category as the rest of
		// the booking flow so a payer who muted booking emails on web
		// isn't surprised by these.
		case event.SplitShareAuthorized:
			amount := fmt.Sprintf("%.2f %s", float64(ev.AmountCents)/100, ev.Currency)
			s.send(ctx, ev.PayerID, catBookings, "Your share of a group booking is on hold",
				fmt.Sprintf("We've placed a hold of %s for your share of a group booking. The trip is booked once everyone pays.", amount))

		case event.SplitShareRefunded:
			s.send(ctx, ev.PayerID, catBookings, "Your share was refunded",
				"We've refunded your portion of a cancelled booking. Funds should reach your card within a few business days.")

		// Offer lifecycle (S106 — WF-GAP-008 email arm). S99 wired the
		// in-app notification + realtime push; this is the transactional
		// email so a guest who muted in-app push but kept email opt-in
		// still hears about the offer. Same opt-out path (catBookings) as
		// the other booking-flow events — Created → guest, Declined →
		// host, Withdrawn → guest.
		case event.OfferCreated:
			title := propertyTitleOr(ev.PropertyTitle, "a property")
			subject := "New offer"
			body := fmt.Sprintf("A host sent you an offer for %q. Open the app to review it.", title)
			if ev.Kind == "pre_approval" {
				subject = "You're pre-approved"
				body = fmt.Sprintf("A host pre-approved you to book %q. Open the app to confirm the dates.", title)
			}
			s.send(ctx, ev.GuestID, catBookings, subject, body)

		case event.OfferDeclined:
			title := propertyTitleOr(ev.PropertyTitle, "your property")
			s.send(ctx, ev.HostID, catBookings, "Offer declined",
				fmt.Sprintf("The guest declined your offer for %q.", title))

		case event.OfferWithdrawn:
			title := propertyTitleOr(ev.PropertyTitle, "a property")
			s.send(ctx, ev.GuestID, catBookings, "Offer withdrawn",
				fmt.Sprintf("The host withdrew their offer for %q.", title))

		// Co-host invitation (S111 — email arm for WF-GAP-016). S99 wired the
		// in-app notification + realtime push when a host grants someone
		// cohost permissions; this is the transactional email so the invitee
		// is told even if they have in-app push muted. Routed through
		// catAccount because a cohost grant is an account-level role change,
		// not a per-booking event — same channel as IdentityVerified and the
		// Resolution Center notices.
		case event.CohostInvited:
			title := propertyTitleOr(ev.PropertyTitle, "a property")
			perms := strings.Join(ev.Permissions, ", ")
			s.send(ctx, ev.UserID, catAccount, "You're now a co-host",
				fmt.Sprintf("A host granted you co-host permissions on %q: %s.", title, perms))
		}
	}
}

// handleSplitCompleted emails the organizer and every other payer that the
// group's split-payment plan is now fully funded. Routed through catBookings
// so users who muted booking emails are respected. The handler is a no-op if
// the booking/split repositories haven't been wired (kept optional so older
// tests don't need to plumb them through).
func (s *Service) handleSplitCompleted(ctx context.Context, ev event.SplitPaymentCompleted) {
	if s.bookings == nil || s.splits == nil {
		return
	}
	b, err := s.bookings.FindByID(ctx, ev.BookingID)
	if err != nil {
		slog.Error("email split-completed: booking lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	sp, err := s.splits.FindByID(ctx, ev.SplitPaymentID)
	if err != nil {
		slog.Error("email split-completed: split lookup failed", "split", ev.SplitPaymentID, "error", err)
		return
	}
	// Organizer.
	s.send(ctx, b.GuestID, catBookings, "Your trip is booked",
		"Good news — everyone paid their share of the split. Your reservation is confirmed.")
	// Other payers (dedup against the organizer, whose share is also in the list).
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
		s.send(ctx, payerID, catBookings, "Group trip booked",
			"The split-payment plan you contributed to is now fully funded — the trip is confirmed.")
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

// propertyTitleOr mirrors experienceTitleOr for property-titled events
// (offers, in particular — the host may have deleted the listing between
// dispatching and consuming the event). The notification package keeps
// its own copy; sharing the helper across packages isn't worth the seam.
func propertyTitleOr(title, fallback string) string {
	if title == "" {
		return fallback
	}
	return title
}

// SendArrivalInfoEmail emails the guest that their check-in details are now
// visible in the listing (S107 — mirror of the S102 in-app notification).
// Uses catBookings so the existing "mute booking emails" preference applies.
func (s *Service) SendArrivalInfoEmail(ctx context.Context, guestID uuid.UUID, propertyTitle string) error {
	title := propertyTitleOr(propertyTitle, "your stay")
	subject := "Check-in details available"
	body := "Your check-in instructions and wifi details for " + title + " are now visible in the listing."
	s.send(ctx, guestID, catBookings, subject, body)
	// send() doesn't surface a structured error — but the interface in
	// arrivalapp.Emailer expects one. The send helper logs internally; we
	// return nil here so the arrival scheduler's loop continues. If a
	// future revision needs hard error propagation, send can grow a
	// returning twin.
	return nil
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
