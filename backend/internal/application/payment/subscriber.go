package paymentapp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// EventHandler returns an event.Handler that drives payments off the booking
// lifecycle: authorize on request, capture on confirmation, refund on
// cancellation. Failures are logged; the payment's own status records the
// outcome (e.g. failed authorization).
func (s *Service) EventHandler() event.Handler {
	return func(ctx context.Context, e event.Event) {
		switch ev := e.(type) {
		case event.BookingRequested:
			s.authorize(ctx, ev)
		case event.BookingConfirmed:
			s.transition(ctx, ev.BookingID, "capture", func(p *payment.Payment) error {
				if err := s.gateway.Capture(ctx, p.GatewayRef); err != nil {
					return err
				}
				return p.Capture()
			})
		case event.BookingCancelled:
			s.transition(ctx, ev.BookingID, "refund", func(p *payment.Payment) error {
				if p.Status == payment.StatusRefunded || p.Status == payment.StatusFailed {
					return nil // nothing to do
				}
				if err := s.gateway.Refund(ctx, p.GatewayRef); err != nil {
					return err
				}
				return p.Refund()
			})
		}
	}
}

func (s *Service) authorize(ctx context.Context, ev event.BookingRequested) {
	amount, err := shared.NewMoney(ev.TotalCents, ev.Currency)
	if err != nil {
		slog.Error("payment: invalid amount", "booking", ev.BookingID, "error", err)
		return
	}
	p := payment.New(ev.BookingID, ev.GuestID, amount)

	ref, err := s.gateway.Authorize(ctx, amount, ev.BookingID.String())
	if err != nil {
		p.Fail(err.Error())
	} else if err := p.Authorize(ref); err != nil {
		p.Fail(err.Error())
	}
	if err := s.repo.Create(ctx, p); err != nil {
		slog.Error("payment: failed to persist authorization", "booking", ev.BookingID, "error", err)
	}
}

func (s *Service) transition(ctx context.Context, bookingID uuid.UUID, action string, apply func(*payment.Payment) error) {
	p, err := s.repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		if !errors.Is(err, shared.ErrNotFound) {
			slog.Error("payment: lookup failed", "action", action, "booking", bookingID, "error", err)
		}
		return
	}
	if err := apply(p); err != nil {
		slog.Error("payment: "+action+" failed", "booking", bookingID, "error", err)
		return
	}
	if err := s.repo.Update(ctx, p); err != nil {
		slog.Error("payment: persist failed", "action", action, "booking", bookingID, "error", err)
	}
}
