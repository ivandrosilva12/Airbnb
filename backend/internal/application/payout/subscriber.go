package payoutapp

import (
	"context"
	"log/slog"
	"math"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// EventHandler returns an event.Handler that maintains the host-earnings ledger
// off the booking lifecycle: a confirmation credits the host with the net
// payout, a cancellation debits the refunded portion. It is best-effort —
// failures are logged and never propagated to the publishing use case.
func (s *Service) EventHandler() event.Handler {
	return func(ctx context.Context, e event.Event) {
		switch ev := e.(type) {
		case event.BookingConfirmed:
			s.recordEarning(ctx, ev)
		case event.BookingCancelled:
			s.recordRefund(ctx, ev)
		}
	}
}

func (s *Service) recordEarning(ctx context.Context, ev event.BookingConfirmed) {
	// Idempotency: BookingConfirmed may be delivered more than once (at-least-once
	// outbox). Recording the earning twice would inflate the host's balance, so
	// skip if one already exists for this booking.
	if has, err := s.payouts.HasEarningForBooking(ctx, ev.BookingID); err != nil {
		slog.Error("payout: earning lookup failed", "booking", ev.BookingID, "error", err)
		return
	} else if has {
		return
	}
	prop, err := s.properties.FindByID(ctx, ev.PropertyID)
	if err != nil {
		slog.Error("payout: property lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	b, err := s.bookings.FindByID(ctx, ev.BookingID)
	if err != nil {
		slog.Error("payout: booking lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	net, err := hostNet(b)
	if err != nil {
		slog.Error("payout: invalid net amount", "booking", ev.BookingID, "error", err)
		return
	}
	if err := s.payouts.Create(ctx, payout.NewEarning(prop.HostID, b.ID, prop.ID, net)); err != nil {
		slog.Error("payout: persist earning failed", "booking", ev.BookingID, "error", err)
	}
}

func (s *Service) recordRefund(ctx context.Context, ev event.BookingCancelled) {
	// Only realised earnings can be reversed: a booking cancelled before
	// confirmation never credited the host.
	has, err := s.payouts.HasEarningForBooking(ctx, ev.BookingID)
	if err != nil {
		slog.Error("payout: earning lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	if !has {
		return
	}
	fraction := clampFraction(ev.RefundFraction)
	if fraction == 0 {
		return // the host keeps the full charge under the cancellation policy
	}
	// Policy: on a guest cancellation the guest is refunded `fraction` of the
	// full total (payment subscriber) while the host is debited `fraction` of
	// their *net* earning (total − service fee). The platform therefore absorbs
	// `fraction` × service fee — it does not profit from cancelled stays. This
	// asymmetry is intentional; the guest refund and host debit are each correct
	// relative to what that party paid/earned.
	b, err := s.bookings.FindByID(ctx, ev.BookingID)
	if err != nil {
		slog.Error("payout: booking lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	net, err := hostNet(b)
	if err != nil {
		slog.Error("payout: invalid net amount", "booking", ev.BookingID, "error", err)
		return
	}
	refundCents := int64(math.Round(float64(net.AmountCents()) * fraction))
	if refundCents == 0 {
		return
	}
	amount, err := shared.NewMoney(refundCents, net.Currency())
	if err != nil {
		slog.Error("payout: invalid refund amount", "booking", ev.BookingID, "error", err)
		return
	}
	hostID := ev.HostID
	if hostID == uuid.Nil {
		if prop, err := s.properties.FindByID(ctx, ev.PropertyID); err == nil {
			hostID = prop.HostID
		}
	}
	if err := s.payouts.Create(ctx, payout.NewRefund(hostID, ev.BookingID, ev.PropertyID, amount)); err != nil {
		slog.Error("payout: persist refund failed", "booking", ev.BookingID, "error", err)
	}
}

func clampFraction(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
