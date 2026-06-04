package payoutapp

import (
	"context"
	"errors"
	"math"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/observability/logctx"
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
		logctx.LoggerFrom(ctx).Error("payout: earning lookup failed", "booking", ev.BookingID, "error", err)
		return
	} else if has {
		return
	}
	prop, err := s.properties.FindByID(ctx, ev.PropertyID)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("payout: property lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	b, err := s.bookings.FindByID(ctx, ev.BookingID)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("payout: booking lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	net, err := hostNet(b)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("payout: invalid net amount", "booking", ev.BookingID, "error", err)
		return
	}

	// S88 — WF-GAP-004. If the booking is paid by a split-payment plan and
	// we have visibility into the share table, credit the host per share
	// rather than as one lump. Each share contributes its proportional
	// slice of the *net* payout (i.e. the platform service fee is taken
	// off the full booking total first; what remains is what the host
	// actually earns, then distributed per share). The host keeps the
	// same total — only the bookkeeping shifts — so downstream sums are
	// unchanged.
	if s.splitRepo != nil {
		if persisted := s.recordSplitEarnings(ctx, prop, b, net); persisted {
			return
		}
	}

	if err := s.payouts.Create(ctx, payout.NewEarning(prop.HostID, b.ID, prop.ID, net)); err != nil {
		logctx.LoggerFrom(ctx).Error("payout: persist earning failed", "booking", ev.BookingID, "error", err)
	}
}

// recordSplitEarnings emits one earning entry per share when the booking has
// a split. Reports whether the per-share path actually ran (true) so the
// caller can skip the single-lump fallback. A booking with no split, or a
// split with no paid shares (a still-pending split shouldn't normally see a
// BookingConfirmed, but be defensive), falls back to the single-lump path
// to keep the host's balance correct.
func (s *Service) recordSplitEarnings(ctx context.Context, prop *property.Property, b *booking.Booking, net shared.Money) bool {
	sp, err := s.splitRepo.FindByBookingID(ctx, b.ID)
	if err != nil {
		if !errors.Is(err, shared.ErrNotFound) {
			logctx.LoggerFrom(ctx).Error("payout: split lookup failed", "booking", b.ID, "error", err)
		}
		return false // no split (or lookup failed) — fall back to lump payout
	}
	if len(sp.Shares) == 0 {
		return false
	}

	// Each share contributes its proportional slice of the net payout,
	// computed from the share's amount divided by the booking total. The
	// last share absorbs any rounding remainder so the per-share entries
	// always sum exactly to net.AmountCents().
	totalCents := sp.TotalCents
	if totalCents <= 0 {
		return false
	}
	netCents := net.AmountCents()
	var allocated int64
	allocations := make([]int64, len(sp.Shares))
	for i, sh := range sp.Shares {
		if i == len(sp.Shares)-1 {
			allocations[i] = netCents - allocated
			continue
		}
		// Integer math: share.AmountCents * netCents / totalCents.
		allocations[i] = sh.AmountCents * netCents / totalCents
		allocated += allocations[i]
	}

	for i, sh := range sp.Shares {
		amt := allocations[i]
		if amt <= 0 {
			continue
		}
		money, err := shared.NewMoney(amt, net.Currency())
		if err != nil {
			logctx.LoggerFrom(ctx).Error("payout: per-share amount invalid",
				"booking", b.ID, "share", sh.ID, "error", err)
			continue
		}
		if err := s.payouts.Create(ctx, payout.NewEarning(prop.HostID, b.ID, prop.ID, money)); err != nil {
			logctx.LoggerFrom(ctx).Error("payout: persist per-share earning failed",
				"booking", b.ID, "share", sh.ID, "error", err)
			// Don't bail mid-loop: the booking-level HasEarningForBooking
			// guard already protects against the at-least-once retry; an
			// operator can replay the event after fixing the root cause.
		}
	}
	return true
}

func (s *Service) recordRefund(ctx context.Context, ev event.BookingCancelled) {
	// Only realised earnings can be reversed: a booking cancelled before
	// confirmation never credited the host.
	has, err := s.payouts.HasEarningForBooking(ctx, ev.BookingID)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("payout: earning lookup failed", "booking", ev.BookingID, "error", err)
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
		logctx.LoggerFrom(ctx).Error("payout: booking lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	net, err := hostNet(b)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("payout: invalid net amount", "booking", ev.BookingID, "error", err)
		return
	}
	refundCents := int64(math.Round(float64(net.AmountCents()) * fraction))
	if refundCents == 0 {
		return
	}
	amount, err := shared.NewMoney(refundCents, net.Currency())
	if err != nil {
		logctx.LoggerFrom(ctx).Error("payout: invalid refund amount", "booking", ev.BookingID, "error", err)
		return
	}
	hostID := ev.HostID
	if hostID == uuid.Nil {
		if prop, err := s.properties.FindByID(ctx, ev.PropertyID); err == nil {
			hostID = prop.HostID
		}
	}
	if err := s.payouts.Create(ctx, payout.NewRefund(hostID, ev.BookingID, ev.PropertyID, amount)); err != nil {
		logctx.LoggerFrom(ctx).Error("payout: persist refund failed", "booking", ev.BookingID, "error", err)
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
