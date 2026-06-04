// Package paymentapp contains payment use cases. Money movement is driven by the
// booking lifecycle through domain events (authorize on request, capture on
// confirmation, refund on cancellation) and delegated to a PaymentGateway port.
package paymentapp

import (
	"context"
	"errors"
	"github.com/airhost/backend/internal/observability/logctx"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates payment use cases.
type Service struct {
	repo       payment.Repository
	deposits   payment.DepositRepository
	gateway    port.PaymentGateway
	bookings   booking.Repository
	properties property.Repository
	// publisher is used to fan out PaymentAuthorized once an asynchronous
	// gateway webhook completes the authorization. Nil falls back to a
	// no-op publisher so the service still works in tests that don't wire
	// the booking auto-confirm subscriber.
	publisher event.Publisher
}

// NewService wires the payment application service. The booking and property
// repositories are read-only dependencies used to build receipts. Deposit
// support is opt-in via WithDeposits — when no deposit repository is wired
// the deposit subscriber and damage-via-deposit paths are no-ops.
func NewService(repo payment.Repository, gateway port.PaymentGateway, bookings booking.Repository, properties property.Repository) *Service {
	return &Service{repo: repo, gateway: gateway, bookings: bookings, properties: properties, publisher: event.Nop()}
}

// WithDeposits enables the security-deposit flow on the service. Returns the
// same service for chained construction.
func (s *Service) WithDeposits(deposits payment.DepositRepository) *Service {
	s.deposits = deposits
	return s
}

// WithPublisher wires the event publisher used to emit PaymentAuthorized when
// an asynchronous gateway webhook completes the authorization. Without it,
// the post-authorize hook is silent — useful for narrow unit tests that
// only exercise the reconciliation state machine. Returns the same service
// for chained construction.
func (s *Service) WithPublisher(pub event.Publisher) *Service {
	if pub == nil {
		pub = event.Nop()
	}
	s.publisher = pub
	return s
}

// GetDepositForBooking returns the deposit hold for a booking when the actor
// is allowed to see it (the booking's guest, for now — host visibility is
// proxied through the booking detail).
func (s *Service) GetDepositForBooking(ctx context.Context, actorID, bookingID uuid.UUID) (*payment.DepositHold, error) {
	if s.deposits == nil {
		return nil, shared.ErrNotFound
	}
	d, err := s.deposits.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if d.GuestID != actorID {
		return nil, shared.ErrForbidden
	}
	return d, nil
}

// GetForBooking returns the payment for a booking. Only the guest who owns it
// may view it (hosts/admins are handled elsewhere if needed).
func (s *Service) GetForBooking(ctx context.Context, actorID, bookingID uuid.UUID) (*payment.Payment, error) {
	p, err := s.repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if p.GuestID != actorID {
		return nil, shared.ErrForbidden
	}
	return p, nil
}

// ListForGuest returns a guest's payments.
func (s *Service) ListForGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*payment.Payment], error) {
	return s.repo.ListByGuest(ctx, guestID, page)
}

// ReconcileGatewayEvent applies an asynchronous gateway webhook event to the
// local payment, idempotently (gateways retry webhooks). It reports whether a
// state change was persisted. Events for unknown references or already-settled
// payments are no-ops, so re-delivery is safe.
func (s *Service) ReconcileGatewayEvent(ctx context.Context, evt port.GatewayEvent) (bool, error) {
	if evt.Type == port.GatewayIgnored || evt.Reference == "" {
		return false, nil
	}
	p, err := s.findByGatewayRef(ctx, evt.Provider, evt.Reference)
	if err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			logctx.LoggerFrom(ctx).Warn("payment: webhook for unknown reference", "provider", evt.Provider, "ref", evt.Reference)
			return false, nil
		}
		return false, err
	}

	// authorized tracks whether THIS call moved the payment from pending to
	// authorized — used after the persist to emit PaymentAuthorized exactly
	// once per async-auth completion (a duplicate webhook finds the payment
	// already authorized and short-circuits before reaching here).
	authorized := false

	switch evt.Type {
	case port.GatewayAuthorized:
		if p.Status != payment.StatusPending {
			return false, nil // already authorized/captured/failed — duplicate or out-of-order
		}
		ref := p.GatewayRef
		if ref == "" {
			ref = evt.Reference
		}
		if err := p.Authorize(ref); err != nil {
			return false, err
		}
		authorized = true
	case port.GatewayCaptured:
		if p.Status != payment.StatusAuthorized {
			return false, nil // already captured/settled or not capturable
		}
		if err := p.Capture(); err != nil {
			return false, err
		}
	case port.GatewayRefunded:
		if p.Status != payment.StatusAuthorized && p.Status != payment.StatusCaptured {
			return false, nil // already refunded or nothing to refund
		}
		amount := evt.AmountCents
		if amount <= 0 || amount > p.Amount.AmountCents() {
			amount = p.Amount.AmountCents()
		}
		if err := p.Refund(amount); err != nil {
			return false, err
		}
	case port.GatewayFailed:
		if p.Status == payment.StatusCaptured || p.Status == payment.StatusRefunded || p.Status == payment.StatusFailed {
			return false, nil // a settled or already-failed payment is not re-failed
		}
		p.Fail(evt.FailureReason)
	default:
		return false, nil
	}

	if err := s.repo.Update(ctx, p); err != nil {
		return false, err
	}
	if authorized {
		// Async-auth completed: fan out PaymentAuthorized so the booking
		// subscriber can auto-confirm an instant-book reservation that has
		// been stuck in pending while waiting for the gateway. Subscribers
		// guard against duplicates themselves — we already short-circuited
		// the second reconcile above, but the at-least-once outbox can
		// re-deliver this event on its own.
		s.publisher.Publish(ctx, event.PaymentAuthorized{
			BookingID:  p.BookingID,
			PaymentID:  p.ID,
			GuestID:    p.GuestID,
			GatewayRef: p.GatewayRef,
		})
	}
	return true, nil
}

// RefundForBooking applies a partial refund against the payment of the given
// booking, returning amountCents to the guest. The dispute that triggered the
// refund is recorded in the payment_adjustments ledger; re-applying the same
// dispute outcome is a storage-level no-op (HasAdjustmentFor guards in the
// aggregate, ON CONFLICT DO NOTHING in the repo).
//
// This is the dispute side of partial refunds — the booking-cancellation flow
// uses Payment.Refund (one-shot) instead. Cumulative refunds across both
// paths still share the same "sum ≤ captured" cap because RefundPartial
// validates against the running RefundedCents.
func (s *Service) RefundForBooking(ctx context.Context, bookingID uuid.UUID, amountCents int64, reason string, disputeID uuid.UUID) error {
	p, err := s.repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return err
	}
	if p.HasAdjustmentFor(payment.AdjustmentRefund, "dispute", disputeID) {
		return nil // already applied — keep this idempotent
	}
	if err := s.gateway.Refund(ctx, p.GatewayRef, amountCents); err != nil {
		return err
	}
	if _, err := p.RefundPartial(amountCents, reason, "dispute", disputeID); err != nil {
		return err
	}
	return s.repo.Update(ctx, p)
}

// DamageClaimForBooking applies a moderator-awarded damage amount in two
// stages: first it tries to capture as much as possible from the booking's
// security-deposit hold (real gateway money moving to the host), and then —
// if the deposit was missing or insufficient — it records the overflow as an
// audit-only damage_claim row on the rental Payment. Either step is skipped
// when it has nothing to do, so the call stays idempotent.
//
// Returns the (capturedFromDeposit, claimedAsOverflow) split so callers can
// surface the breakdown to the moderator.
func (s *Service) DamageClaimForBooking(ctx context.Context, bookingID uuid.UUID, amountCents int64, reason string, disputeID uuid.UUID) error {
	_, _, err := s.applyDamage(ctx, bookingID, amountCents, reason, disputeID)
	return err
}

func (s *Service) applyDamage(ctx context.Context, bookingID uuid.UUID, amountCents int64, reason string, disputeID uuid.UUID) (int64, int64, error) {
	if amountCents <= 0 {
		return 0, 0, shared.NewValidationError("damage amount must be positive")
	}
	remaining := amountCents
	var captured int64

	// (1) Capture from the deposit hold if one exists and is still active.
	if s.deposits != nil {
		dep, err := s.deposits.FindByBookingID(ctx, bookingID)
		if err == nil && dep != nil {
			if dep.HasAdjustmentFor("dispute", disputeID) {
				return 0, 0, nil // already applied — idempotent
			}
			switch dep.Status {
			case payment.DepositAuthorized, payment.DepositPartiallyCaptured:
				if dep.Remaining() > 0 {
					if _, took, capErr := dep.CapturePartial(remaining, reason, "dispute", disputeID); capErr != nil {
						return 0, 0, capErr
					} else if took > 0 {
						// The hold is partially captured; the gateway needs to settle
						// the captured slice. Refund the unused remainder later (on
						// release). For the captured portion we ask the gateway to
						// turn the hold into a charge — Capture on the gateway is the
						// closest semantics. If the gateway doesn't support partial
						// capture against a hold, treat it as best-effort.
						if capErr := s.gateway.Capture(ctx, dep.GatewayRef); capErr != nil {
							logctx.LoggerFrom(ctx).Warn("deposit: gateway capture failed", "booking", bookingID, "error", capErr)
						}
						captured += took
						remaining -= took
					}
				}
				if err := s.deposits.Update(ctx, dep); err != nil {
					return 0, 0, err
				}
			}
		} else if err != nil && !errors.Is(err, shared.ErrNotFound) {
			return 0, 0, err
		}
	}

	// (2) Anything left becomes an audit row on the rental payment.
	if remaining > 0 {
		p, err := s.repo.FindByBookingID(ctx, bookingID)
		if err != nil {
			return captured, 0, err
		}
		if !p.HasAdjustmentFor(payment.AdjustmentDamageClaim, "dispute", disputeID) {
			if _, err := p.RecordDamageClaim(remaining, reason, "dispute", disputeID); err != nil {
				return captured, 0, err
			}
			if err := s.repo.Update(ctx, p); err != nil {
				return captured, 0, err
			}
		}
	}
	return captured, remaining, nil
}

// findByGatewayRef resolves a payment from a provider-native reference, allowing
// for the routing gateway's "<provider>:<ref>" tag on the stored value.
func (s *Service) findByGatewayRef(ctx context.Context, provider, ref string) (*payment.Payment, error) {
	candidates := []string{ref}
	if provider != "" {
		candidates = []string{provider + ":" + ref, ref}
	}
	var lastErr error
	for _, c := range candidates {
		p, err := s.repo.FindByGatewayRef(ctx, c)
		if err == nil {
			return p, nil
		}
		lastErr = err
		if !errors.Is(err, shared.ErrNotFound) {
			return nil, err
		}
	}
	return nil, lastErr
}

// ReceiptData is the read-model rendered into a payment receipt.
//
// JurisdictionTaxLines is the per-rule breakdown the booking carries (S49)
// — the interface layer renders these as itemised lines on the PDF (S58).
// The Tax field stays for bookings written before S49 where no rules matched.
type ReceiptData struct {
	ReceiptNo            string
	IssuedAt             time.Time
	Status               payment.Status
	PropertyTitle        string
	CheckIn              time.Time
	CheckOut             time.Time
	Nights               int
	Guests               int
	Subtotal             shared.Money
	Discount             shared.Money
	CleaningFee          shared.Money
	ServiceFee           shared.Money
	Tax                  shared.Money
	JurisdictionTaxLines []booking.TaxLine
	Total                shared.Money
}

// Receipt assembles the data for a booking's payment receipt. Only the guest who
// owns the payment may obtain it.
func (s *Service) Receipt(ctx context.Context, actorID, bookingID uuid.UUID) (ReceiptData, error) {
	p, err := s.repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return ReceiptData{}, err
	}
	if p.GuestID != actorID {
		return ReceiptData{}, shared.ErrForbidden
	}
	b, err := s.bookings.FindByID(ctx, bookingID)
	if err != nil {
		return ReceiptData{}, err
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return ReceiptData{}, err
	}
	// Defensive copy so the receipt is independent of the booking's slice.
	var taxLines []booking.TaxLine
	if n := len(b.Pricing.JurisdictionTaxLines); n > 0 {
		taxLines = make([]booking.TaxLine, n)
		copy(taxLines, b.Pricing.JurisdictionTaxLines)
	}
	return ReceiptData{
		ReceiptNo:            p.ID.String(),
		IssuedAt:             time.Now().UTC(),
		Status:               p.Status,
		PropertyTitle:        prop.Title,
		CheckIn:              b.Dates.CheckIn,
		CheckOut:             b.Dates.CheckOut,
		Nights:               b.Dates.Nights(),
		Guests:               b.Guests,
		Subtotal:             b.Pricing.Subtotal,
		Discount:             b.Pricing.Discount,
		CleaningFee:          b.Pricing.CleaningFee,
		ServiceFee:            b.Pricing.ServiceFee,
		Tax:                  b.Pricing.Tax,
		JurisdictionTaxLines: taxLines,
		Total:                b.Pricing.Total,
	}, nil
}
