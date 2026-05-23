package paymentapp

import (
	"context"
	"errors"
	"log/slog"
	"math"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func clampFraction(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

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
			fraction := ev.RefundFraction
			s.transition(ctx, ev.BookingID, "refund", func(p *payment.Payment) error {
				switch p.Status {
				case payment.StatusAuthorized:
					// Nothing was charged yet — release the whole hold.
					if err := s.gateway.Refund(ctx, p.GatewayRef, p.Amount.AmountCents()); err != nil {
						return err
					}
					return p.Refund(p.Amount.AmountCents())
				case payment.StatusCaptured:
					refund := int64(math.Round(float64(p.Amount.AmountCents()) * clampFraction(fraction)))
					if refund == 0 {
						return nil // policy grants no refund; the host keeps the charge
					}
					if err := s.gateway.Refund(ctx, p.GatewayRef, refund); err != nil {
						return err
					}
					return p.Refund(refund)
				default:
					return nil // pending / refunded / failed: nothing to do
				}
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
