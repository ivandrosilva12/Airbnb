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
//
// When deposit support is wired (Service.WithDeposits) the same handler also
// places a security-deposit hold on BookingConfirmed (after a successful
// rental capture) and releases the remainder on BookingCompleted or
// BookingCancelled.
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
			s.placeDepositHold(ctx, ev)
		case event.BookingModified:
			s.reauthorize(ctx, ev)
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
			s.releaseDepositHold(ctx, ev.BookingID)
		case event.BookingCompleted:
			s.releaseDepositHold(ctx, ev.BookingID)
		}
	}
}

// placeDepositHold authorizes a security-deposit hold when the property is
// configured with one. Idempotent: a second BookingConfirmed delivery is a
// no-op because the deposit row already exists for the booking.
func (s *Service) placeDepositHold(ctx context.Context, ev event.BookingConfirmed) {
	if s.deposits == nil {
		return
	}
	if _, err := s.deposits.FindByBookingID(ctx, ev.BookingID); err == nil {
		return // already placed
	} else if !errors.Is(err, shared.ErrNotFound) {
		slog.Error("deposit: lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	b, err := s.bookings.FindByID(ctx, ev.BookingID)
	if err != nil {
		slog.Error("deposit: booking lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		slog.Error("deposit: property lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	if prop.SecurityDeposit.AmountCents() <= 0 {
		return // listing doesn't require a deposit
	}
	d, err := payment.NewDepositHold(b.ID, b.GuestID, prop.SecurityDeposit)
	if err != nil {
		slog.Error("deposit: invalid amount", "booking", ev.BookingID, "error", err)
		return
	}
	ref, gErr := s.gateway.Authorize(ctx, prop.SecurityDeposit, "deposit:"+b.ID.String())
	if gErr != nil {
		d.Fail(gErr.Error())
	} else if err := d.Authorize(ref); err != nil {
		d.Fail(err.Error())
	}
	if err := s.deposits.Create(ctx, d); err != nil {
		slog.Error("deposit: persist failed", "booking", ev.BookingID, "error", err)
	}
}

// releaseDepositHold returns the remaining hold to the guest after a clean
// stay or a cancellation. A captured / released / failed hold is skipped
// (terminal); a missing hold is a no-op (idempotent across re-deliveries).
func (s *Service) releaseDepositHold(ctx context.Context, bookingID uuid.UUID) {
	if s.deposits == nil {
		return
	}
	d, err := s.deposits.FindByBookingID(ctx, bookingID)
	if err != nil {
		if !errors.Is(err, shared.ErrNotFound) {
			slog.Error("deposit: lookup failed on release", "booking", bookingID, "error", err)
		}
		return
	}
	switch d.Status {
	case payment.DepositAuthorized, payment.DepositPartiallyCaptured:
		if d.Remaining() > 0 {
			if err := s.gateway.Refund(ctx, d.GatewayRef, d.Remaining()); err != nil {
				slog.Error("deposit: gateway refund failed", "booking", bookingID, "error", err)
				return
			}
		}
		if err := d.Release(); err != nil {
			slog.Error("deposit: release failed", "booking", bookingID, "error", err)
			return
		}
		if err := s.deposits.Update(ctx, d); err != nil {
			slog.Error("deposit: persist release failed", "booking", bookingID, "error", err)
		}
	}
}

func (s *Service) authorize(ctx context.Context, ev event.BookingRequested) {
	// Idempotency: BookingRequested may be delivered more than once (at-least-once
	// outbox). If a payment already exists for the booking it was authorized;
	// re-authorizing would create a duplicate hold at the gateway, so skip.
	if _, err := s.repo.FindByBookingID(ctx, ev.BookingID); err == nil {
		return
	} else if !errors.Is(err, shared.ErrNotFound) {
		slog.Error("payment: lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
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

// reauthorize adjusts the outstanding hold after a booking is modified: it
// releases the old authorization and places a new one for the updated total.
// Only an authorized (not yet captured) payment can be adjusted this way.
//
// Idempotency: BookingModified may be delivered more than once. Once the hold
// already matches the new total there is nothing to do, so a redelivery is a
// no-op. (The release+re-hold is two gateway calls and not atomic; a crash
// between them leaves the booking without a hold, which reconciliation/retry of
// the original authorize path would surface — acceptable for a pending booking.)
func (s *Service) reauthorize(ctx context.Context, ev event.BookingModified) {
	p, err := s.repo.FindByBookingID(ctx, ev.BookingID)
	if err != nil {
		if !errors.Is(err, shared.ErrNotFound) {
			slog.Error("payment: lookup failed", "action", "reauthorize", "booking", ev.BookingID, "error", err)
		}
		return
	}
	if p.Status != payment.StatusAuthorized {
		return // captured/refunded/failed: the hold can no longer be adjusted
	}
	newAmount, err := shared.NewMoney(ev.TotalCents, ev.Currency)
	if err != nil {
		slog.Error("payment: invalid amount", "action", "reauthorize", "booking", ev.BookingID, "error", err)
		return
	}
	if newAmount.AmountCents() == p.Amount.AmountCents() {
		return // total unchanged (or a redelivery already applied it)
	}
	if err := s.gateway.Refund(ctx, p.GatewayRef, p.Amount.AmountCents()); err != nil {
		slog.Error("payment: releasing old hold failed", "booking", ev.BookingID, "error", err)
		return
	}
	ref, err := s.gateway.Authorize(ctx, newAmount, ev.BookingID.String())
	if err != nil {
		p.Fail(err.Error())
	} else if err := p.Reauthorize(ref, newAmount); err != nil {
		p.Fail(err.Error())
	}
	if err := s.repo.Update(ctx, p); err != nil {
		slog.Error("payment: persist re-authorization failed", "booking", ev.BookingID, "error", err)
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
