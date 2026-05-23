// Package paymentapp contains payment use cases. Money movement is driven by the
// booking lifecycle through domain events (authorize on request, capture on
// confirmation, refund on cancellation) and delegated to a PaymentGateway port.
package paymentapp

import (
	"context"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates payment use cases.
type Service struct {
	repo    payment.Repository
	gateway port.PaymentGateway
}

// NewService wires the payment application service.
func NewService(repo payment.Repository, gateway port.PaymentGateway) *Service {
	return &Service{repo: repo, gateway: gateway}
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
