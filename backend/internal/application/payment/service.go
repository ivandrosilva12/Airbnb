// Package paymentapp contains payment use cases. Money movement is driven by the
// booking lifecycle through domain events (authorize on request, capture on
// confirmation, refund on cancellation) and delegated to a PaymentGateway port.
package paymentapp

import (
	"context"
	"errors"
	"log/slog"
	"time"

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
	gateway    port.PaymentGateway
	bookings   booking.Repository
	properties property.Repository
}

// NewService wires the payment application service. The booking and property
// repositories are read-only dependencies used to build receipts.
func NewService(repo payment.Repository, gateway port.PaymentGateway, bookings booking.Repository, properties property.Repository) *Service {
	return &Service{repo: repo, gateway: gateway, bookings: bookings, properties: properties}
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
			slog.Warn("payment: webhook for unknown reference", "provider", evt.Provider, "ref", evt.Reference)
			return false, nil
		}
		return false, err
	}

	switch evt.Type {
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

// DamageClaimForBooking records the moderator-awarded damage compensation
// against the booking's payment. Pure audit — no gateway call is made because
// charging a guest for damage requires a security-deposit hold, which is a
// future slice.
func (s *Service) DamageClaimForBooking(ctx context.Context, bookingID uuid.UUID, amountCents int64, reason string, disputeID uuid.UUID) error {
	p, err := s.repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return err
	}
	if p.HasAdjustmentFor(payment.AdjustmentDamageClaim, "dispute", disputeID) {
		return nil
	}
	if _, err := p.RecordDamageClaim(amountCents, reason, "dispute", disputeID); err != nil {
		return err
	}
	return s.repo.Update(ctx, p)
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
type ReceiptData struct {
	ReceiptNo     string
	IssuedAt      time.Time
	Status        payment.Status
	PropertyTitle string
	CheckIn       time.Time
	CheckOut      time.Time
	Nights        int
	Guests        int
	Subtotal      shared.Money
	Discount      shared.Money
	CleaningFee   shared.Money
	ServiceFee    shared.Money
	Tax           shared.Money
	Total         shared.Money
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
	return ReceiptData{
		ReceiptNo:     p.ID.String(),
		IssuedAt:      time.Now().UTC(),
		Status:        p.Status,
		PropertyTitle: prop.Title,
		CheckIn:       b.Dates.CheckIn,
		CheckOut:      b.Dates.CheckOut,
		Nights:        b.Dates.Nights(),
		Guests:        b.Guests,
		Subtotal:      b.Pricing.Subtotal,
		Discount:      b.Pricing.Discount,
		CleaningFee:   b.Pricing.CleaningFee,
		ServiceFee:    b.Pricing.ServiceFee,
		Tax:           b.Pricing.Tax,
		Total:         b.Pricing.Total,
	}, nil
}
