package paymentapp

import (
	"context"
	"errors"
	"math"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/observability/logctx"
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
			if ev.Split {
				return // split bookings settle through splitpayment, not the gateway
			}
			s.authorize(ctx, ev)
		case event.BookingConfirmed:
			if ev.Split {
				return // see BookingRequested above; no capture or deposit hold either
			}
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
			if ev.Split {
				// Split bookings don't hold a Payment row; the splitpayment
				// context handles refunds (out of scope for this slice).
				return
			}
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
		case experiencebooking.ExperienceBookingCreated:
			// S87 — WF-GAP-015. An experience booking authorises the same
			// way a property booking does: by BookingID, for the all-in
			// total. The payment repo keys on BookingID alone so the two
			// flows share storage without colliding (experience bookings
			// generate fresh UUIDs from a different table).
			s.authorizeExperience(ctx, ev)
		case experiencebooking.ExperienceBookingConfirmed:
			// Capture mirrors BookingConfirmed: turn the authorised hold
			// into a charge. No deposit hold here — experiences are not
			// physical stays so there's nothing to claim against.
			s.transition(ctx, ev.BookingID, "capture", func(p *payment.Payment) error {
				if err := s.gateway.Capture(ctx, p.GatewayRef); err != nil {
					return err
				}
				return p.Capture()
			})
		case experiencebooking.ExperienceBookingCancelled:
			// Refund mirrors the BookingCancelled conditional pattern: only
			// act when a payment record exists for the booking (it is
			// missing for experience bookings that pre-date the payment
			// subscriber, or whose Created event failed authorisation).
			// Experiences have no cancellation-policy ladder in this slice
			// — release authorised holds in full and refund captured
			// charges in full. A future slice can plug in a fraction.
			s.refundExperience(ctx, ev.BookingID)
		}
	}
}

// authorizeExperience places a hold for an experience booking. It mirrors
// the property-booking authorize() exactly — same idempotency on a
// duplicate Created delivery, same Fail-on-gateway-error path so the
// payment row records the outcome — only the source event type differs.
func (s *Service) authorizeExperience(ctx context.Context, ev experiencebooking.ExperienceBookingCreated) {
	if _, err := s.repo.FindByBookingID(ctx, ev.BookingID); err == nil {
		return
	} else if !errors.Is(err, shared.ErrNotFound) {
		logctx.LoggerFrom(ctx).Error("payment: lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	amount, err := shared.NewMoney(ev.TotalCents, ev.Currency)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("payment: invalid amount", "booking", ev.BookingID, "error", err)
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
		logctx.LoggerFrom(ctx).Error("payment: failed to persist authorization", "booking", ev.BookingID, "error", err)
	}
}

// refundExperience releases an authorised hold or refunds a captured
// charge in full. Missing payment (gateway never authorised) is a no-op,
// keeping the handler safe against duplicate event deliveries.
func (s *Service) refundExperience(ctx context.Context, bookingID uuid.UUID) {
	s.transition(ctx, bookingID, "refund", func(p *payment.Payment) error {
		switch p.Status {
		case payment.StatusAuthorized:
			if err := s.gateway.Refund(ctx, p.GatewayRef, p.Amount.AmountCents()); err != nil {
				return err
			}
			return p.Refund(p.Amount.AmountCents())
		case payment.StatusCaptured:
			if err := s.gateway.Refund(ctx, p.GatewayRef, p.Amount.AmountCents()); err != nil {
				return err
			}
			return p.Refund(p.Amount.AmountCents())
		default:
			return nil // pending / refunded / failed: nothing to do
		}
	})
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
		logctx.LoggerFrom(ctx).Error("deposit: lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	b, err := s.bookings.FindByID(ctx, ev.BookingID)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("deposit: booking lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("deposit: property lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	if prop.SecurityDeposit.AmountCents() <= 0 {
		return // listing doesn't require a deposit
	}
	d, err := payment.NewDepositHold(b.ID, b.GuestID, prop.SecurityDeposit)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("deposit: invalid amount", "booking", ev.BookingID, "error", err)
		return
	}
	ref, gErr := s.gateway.Authorize(ctx, prop.SecurityDeposit, "deposit:"+b.ID.String())
	if gErr != nil {
		d.Fail(gErr.Error())
	} else if err := d.Authorize(ref); err != nil {
		d.Fail(err.Error())
	}
	if err := s.deposits.Create(ctx, d); err != nil {
		logctx.LoggerFrom(ctx).Error("deposit: persist failed", "booking", ev.BookingID, "error", err)
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
			logctx.LoggerFrom(ctx).Error("deposit: lookup failed on release", "booking", bookingID, "error", err)
		}
		return
	}
	switch d.Status {
	case payment.DepositAuthorized, payment.DepositPartiallyCaptured:
		if d.Remaining() > 0 {
			if err := s.gateway.Refund(ctx, d.GatewayRef, d.Remaining()); err != nil {
				logctx.LoggerFrom(ctx).Error("deposit: gateway refund failed", "booking", bookingID, "error", err)
				return
			}
		}
		if err := d.Release(); err != nil {
			logctx.LoggerFrom(ctx).Error("deposit: release failed", "booking", bookingID, "error", err)
			return
		}
		if err := s.deposits.Update(ctx, d); err != nil {
			logctx.LoggerFrom(ctx).Error("deposit: persist release failed", "booking", bookingID, "error", err)
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
		logctx.LoggerFrom(ctx).Error("payment: lookup failed", "booking", ev.BookingID, "error", err)
		return
	}
	amount, err := shared.NewMoney(ev.TotalCents, ev.Currency)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("payment: invalid amount", "booking", ev.BookingID, "error", err)
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
		logctx.LoggerFrom(ctx).Error("payment: failed to persist authorization", "booking", ev.BookingID, "error", err)
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
			logctx.LoggerFrom(ctx).Error("payment: lookup failed", "action", "reauthorize", "booking", ev.BookingID, "error", err)
		}
		return
	}
	if p.Status != payment.StatusAuthorized {
		return // captured/refunded/failed: the hold can no longer be adjusted
	}
	newAmount, err := shared.NewMoney(ev.TotalCents, ev.Currency)
	if err != nil {
		logctx.LoggerFrom(ctx).Error("payment: invalid amount", "action", "reauthorize", "booking", ev.BookingID, "error", err)
		return
	}
	if newAmount.AmountCents() == p.Amount.AmountCents() {
		return // total unchanged (or a redelivery already applied it)
	}
	if err := s.gateway.Refund(ctx, p.GatewayRef, p.Amount.AmountCents()); err != nil {
		logctx.LoggerFrom(ctx).Error("payment: releasing old hold failed", "booking", ev.BookingID, "error", err)
		return
	}
	ref, err := s.gateway.Authorize(ctx, newAmount, ev.BookingID.String())
	if err != nil {
		p.Fail(err.Error())
	} else if err := p.Reauthorize(ref, newAmount); err != nil {
		p.Fail(err.Error())
	}
	if err := s.repo.Update(ctx, p); err != nil {
		logctx.LoggerFrom(ctx).Error("payment: persist re-authorization failed", "booking", ev.BookingID, "error", err)
	}
}

func (s *Service) transition(ctx context.Context, bookingID uuid.UUID, action string, apply func(*payment.Payment) error) {
	p, err := s.repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		if !errors.Is(err, shared.ErrNotFound) {
			logctx.LoggerFrom(ctx).Error("payment: lookup failed", "action", action, "booking", bookingID, "error", err)
		}
		return
	}
	if err := apply(p); err != nil {
		logctx.LoggerFrom(ctx).Error("payment: "+action+" failed", "booking", bookingID, "error", err)
		return
	}
	if err := s.repo.Update(ctx, p); err != nil {
		logctx.LoggerFrom(ctx).Error("payment: persist failed", "action", action, "booking", bookingID, "error", err)
	}
}
