// Package realtimeapp maps domain events to live updates pushed to the affected
// user, so web/mobile clients can refresh their notification and message badges
// without polling. It reacts to the same events as the notification context.
package realtimeapp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/google/uuid"
)

// Broadcaster delivers an opaque payload to a user's live connections. The
// in-process hub implements it; an external bus could replace it transparently.
type Broadcaster interface {
	Publish(userID uuid.UUID, payload string)
}

// Service translates domain events into realtime client hints.
type Service struct {
	hub      Broadcaster
	bookings booking.Repository      // optional — only used by the split-payment subscriber
	splits   splitpayment.Repository // optional — only used by the split-payment subscriber
}

// NewService wires the realtime application service.
func NewService(hub Broadcaster) *Service { return &Service{hub: hub} }

// WithBookings attaches a booking repository so the event subscriber can
// resolve the organizer on split-payment completion events. Optional — the
// handler short-circuits when nil.
func (s *Service) WithBookings(r booking.Repository) *Service {
	s.bookings = r
	return s
}

// WithSplitPayments attaches a split-payment repository so the realtime hub
// can push a refresh hint to every payer when a split completes. Optional.
func (s *Service) WithSplitPayments(r splitpayment.Repository) *Service {
	s.splits = r
	return s
}

// Update is the payload pushed to clients. Type tells the client which view to
// refresh ("notification" or "message"); it intentionally carries no PII.
// ConversationID is set on message updates so an open chat thread can refresh
// itself the instant a message for it arrives (rather than polling).
type Update struct {
	Type           string `json:"type"`
	ConversationID string `json:"conversationId,omitempty"`
}

// EventHandler returns an event.Handler that pushes a hint to the user affected
// by each event. It is best-effort and never blocks the publishing use case.
func (s *Service) EventHandler() event.Handler {
	return func(ctx context.Context, e event.Event) {
		switch ev := e.(type) {
		case event.BookingRequested:
			s.push(ev.HostID, Update{Type: "notification"})
		case event.BookingConfirmed:
			s.push(ev.GuestID, Update{Type: "notification"})
		case event.BookingCancelled:
			recipient := ev.GuestID
			if ev.CancelledBy == ev.GuestID {
				recipient = ev.HostID
			}
			s.push(recipient, Update{Type: "notification"})

		// S165 — closes the Wave-17 audit gap on the BookingModified
		// realtime arm. The guest is the one modifying; the host needs
		// the badge to refresh immediately rather than waiting up to 30s
		// for the next inbox poll. Mirrors the BookingRequested routing
		// (host is the affected party for guest-initiated booking
		// changes).
		case event.BookingModified:
			s.push(ev.HostID, Update{Type: "notification"})
		case event.MessageSent:
			s.push(ev.RecipientID, Update{Type: "message", ConversationID: ev.ConversationID.String()})
		case event.IdentityVerified:
			s.push(ev.UserID, Update{Type: "notification"})

		// Resolution Center events (S99 / WF-GAP-002) — push the badge
		// to whoever didn't trigger the case. DisputeOpened goes to the
		// respondent (the OTHER party from the opener), DisputeResolved
		// fans out to both guest and host so each sees the decision in
		// their inbox without polling.
		case event.DisputeOpened:
			respondent := ev.HostID
			if ev.OpenerID == ev.HostID {
				respondent = ev.GuestID
			}
			s.push(respondent, Update{Type: "notification"})
		case event.DisputeResolved:
			s.push(ev.GuestID, Update{Type: "notification"})
			s.push(ev.HostID, Update{Type: "notification"})

		// Offer lifecycle (S99 / WF-GAP-008) — push the badge to whichever
		// party isn't the one who acted.
		case event.OfferCreated:
			s.push(ev.GuestID, Update{Type: "notification"})
		case event.OfferDeclined:
			s.push(ev.HostID, Update{Type: "notification"})
		case event.OfferWithdrawn:
			s.push(ev.GuestID, Update{Type: "notification"})

		// Co-host invitation (S99 / WF-GAP-016) — push the badge to the
		// invitee so their cohost mailbox lights up the moment a host
		// grants them access.
		case event.CohostInvited:
			s.push(ev.UserID, Update{Type: "notification"})

		// Experience-booking lifecycle (S86) — push the same notification
		// hint as for property bookings so the inbox badge refreshes
		// without polling.
		case experiencebooking.ExperienceBookingCreated:
			s.push(ev.HostID, Update{Type: "notification"})
		case experiencebooking.ExperienceBookingConfirmed:
			s.push(ev.GuestID, Update{Type: "notification"})
		case experiencebooking.ExperienceBookingCancelled:
			recipient := ev.GuestID
			if ev.CancelledBy == ev.GuestID {
				recipient = ev.HostID
			}
			s.push(recipient, Update{Type: "notification"})

		// Split-payment completion (S93 — WF-GAP-011) fans a notification
		// hint to the organizer and every other payer so the whole group's
		// trips/splits views refresh.
		case event.SplitPaymentCompleted:
			s.handleSplitCompleted(ctx, ev)
		}
	}
}

// handleSplitCompleted pushes a notification-refresh hint to the organizer
// and every other payer, so their split-pay and trips views update without a
// poll. No-op when the booking/split repos haven't been wired.
func (s *Service) handleSplitCompleted(ctx context.Context, ev event.SplitPaymentCompleted) {
	if s.bookings == nil || s.splits == nil {
		return
	}
	b, err := s.bookings.FindByID(ctx, ev.BookingID)
	if err != nil {
		slog.Error("realtime split-completed: booking lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	sp, err := s.splits.FindByID(ctx, ev.SplitPaymentID)
	if err != nil {
		slog.Error("realtime split-completed: split lookup failed", "split", ev.SplitPaymentID, "error", err)
		return
	}
	s.push(b.GuestID, Update{Type: "notification"})
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
		s.push(payerID, Update{Type: "notification"})
	}
}

func (s *Service) push(userID uuid.UUID, update Update) {
	payload, err := json.Marshal(update)
	if err != nil {
		return
	}
	s.hub.Publish(userID, string(payload))
}
