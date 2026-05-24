// Package realtimeapp maps domain events to live updates pushed to the affected
// user, so web/mobile clients can refresh their notification and message badges
// without polling. It reacts to the same events as the notification context.
package realtimeapp

import (
	"context"
	"encoding/json"

	"github.com/airhost/backend/internal/application/event"
	"github.com/google/uuid"
)

// Broadcaster delivers an opaque payload to a user's live connections. The
// in-process hub implements it; an external bus could replace it transparently.
type Broadcaster interface {
	Publish(userID uuid.UUID, payload string)
}

// Service translates domain events into realtime client hints.
type Service struct {
	hub Broadcaster
}

// NewService wires the realtime application service.
func NewService(hub Broadcaster) *Service { return &Service{hub: hub} }

// Update is the payload pushed to clients. Type tells the client which view to
// refresh ("notification" or "message"); it intentionally carries no PII.
type Update struct {
	Type string `json:"type"`
}

// EventHandler returns an event.Handler that pushes a hint to the user affected
// by each event. It is best-effort and never blocks the publishing use case.
func (s *Service) EventHandler() event.Handler {
	return func(_ context.Context, e event.Event) {
		switch ev := e.(type) {
		case event.BookingRequested:
			s.push(ev.HostID, "notification")
		case event.BookingConfirmed:
			s.push(ev.GuestID, "notification")
		case event.BookingCancelled:
			recipient := ev.GuestID
			if ev.CancelledBy == ev.GuestID {
				recipient = ev.HostID
			}
			s.push(recipient, "notification")
		case event.MessageSent:
			s.push(ev.RecipientID, "message")
		case event.IdentityVerified:
			s.push(ev.UserID, "notification")
		}
	}
}

func (s *Service) push(userID uuid.UUID, typ string) {
	payload, err := json.Marshal(Update{Type: typ})
	if err != nil {
		return
	}
	s.hub.Publish(userID, string(payload))
}
