// Package splitpaymentapp orchestrates split-payment use cases.
//
// The service handles the per-share authorisation flow and publishes
// SplitPaymentCompleted when every share is paid. The booking context
// subscribes to that event and confirms the corresponding booking — keeping
// the two domains cleanly separated.
package splitpaymentapp

import (
	"context"
	"errors"
	"strings"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// Service orchestrates split-payment use cases.
type Service struct {
	splits    splitpayment.Repository
	users     user.Repository
	publisher event.Publisher
}

// NewService wires the split-payment application service. The publisher is
// used to fan out SplitPaymentCompleted when the final share is authorised;
// pass event.Nop() to disable event emission (useful in unit tests).
func NewService(splits splitpayment.Repository, users user.Repository, publisher event.Publisher) *Service {
	if publisher == nil {
		publisher = event.Nop()
	}
	return &Service{splits: splits, users: users, publisher: publisher}
}

// CreateInput is what bookingapp passes to seed a new split alongside a
// freshly-created booking. The booking total + currency are authoritative;
// the application enforces that ShareInputs match them.
type CreateInput struct {
	BookingID      uuid.UUID
	OrganizerID    uuid.UUID
	OrganizerEmail string
	Currency       string
	TotalCents     int64
	Shares         []splitpayment.ShareInput
}

// Create persists a new split-payment plan. The aggregate enforces the
// invariants (sum matches, organizer included, etc); this method just
// translates the booking-context inputs and saves.
func (s *Service) Create(ctx context.Context, in CreateInput) (*splitpayment.SplitPayment, error) {
	sp, err := splitpayment.New(in.BookingID, in.OrganizerID, in.OrganizerEmail, in.Currency, in.TotalCents, in.Shares)
	if err != nil {
		return nil, err
	}
	if err := s.splits.Create(ctx, sp); err != nil {
		return nil, err
	}
	return sp, nil
}

// AuthorizeShare marks one share as paid. The actor's email is matched
// (case-insensitively) against the share's invited address — a user with an
// account whose email matches CAN authorise a share even if they aren't
// listed by user id (the domain only knows emails at creation time).
//
// When the final share transitions to paid, the SplitPayment is moved to
// completed and a SplitPaymentCompleted event is published so the booking
// context can confirm the reservation.
func (s *Service) AuthorizeShare(ctx context.Context, actorID, splitID, shareID uuid.UUID) (*splitpayment.SplitPayment, error) {
	actor, err := s.users.FindByID(ctx, actorID)
	if err != nil {
		return nil, err
	}
	sp, err := s.splits.FindByID(ctx, splitID)
	if err != nil {
		return nil, err
	}
	if err := sp.MarkSharePaid(shareID, actorID, actor.Email); err != nil {
		return nil, err
	}
	completed := false
	if sp.AllPaid() {
		if err := sp.MarkCompleted(); err != nil {
			return nil, err
		}
		completed = true
	}
	if err := s.splits.Update(ctx, sp); err != nil {
		return nil, err
	}
	if completed {
		s.publisher.Publish(ctx, event.SplitPaymentCompleted{
			SplitPaymentID: sp.ID,
			BookingID:      sp.BookingID,
		})
	}
	return sp, nil
}

// Cancel transitions a pending split to cancelled. Only the organizer can
// cancel; once cancelled the booking is also cancelled (handled by the
// caller — bookingapp.Service.Cancel for now, which sees the SplitPayment
// state and skips the gateway refund path).
func (s *Service) Cancel(ctx context.Context, actorID, splitID uuid.UUID) (*splitpayment.SplitPayment, error) {
	sp, err := s.splits.FindByID(ctx, splitID)
	if err != nil {
		return nil, err
	}
	if sp.OrganizerID != actorID {
		return nil, shared.ErrForbidden
	}
	if err := sp.Cancel(); err != nil {
		return nil, err
	}
	if err := s.splits.Update(ctx, sp); err != nil {
		return nil, err
	}
	return sp, nil
}

// GetByID returns the split if the actor is allowed to see it (organizer or
// a share payer matched by email). Used by the per-payer view that shows
// "my share to pay" before authorise.
func (s *Service) GetByID(ctx context.Context, actorID, splitID uuid.UUID) (*splitpayment.SplitPayment, error) {
	sp, err := s.splits.FindByID(ctx, splitID)
	if err != nil {
		return nil, err
	}
	if sp.OrganizerID == actorID {
		return sp, nil
	}
	actor, err := s.users.FindByID(ctx, actorID)
	if err != nil {
		// User-not-found masks the split's existence for an authenticated
		// caller — return forbidden so we don't leak via differing codes.
		if errors.Is(err, shared.ErrNotFound) {
			return nil, shared.ErrForbidden
		}
		return nil, err
	}
	if !sp.HasPayer(actor.Email) {
		return nil, shared.ErrForbidden
	}
	return sp, nil
}

// GetByBookingID is the same as GetByID but keyed on the booking, used by
// the booking detail page to fetch its attached split (if any).
func (s *Service) GetByBookingID(ctx context.Context, actorID, bookingID uuid.UUID) (*splitpayment.SplitPayment, error) {
	sp, err := s.splits.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	return s.GetByID(ctx, actorID, sp.ID)
}

// ListMine returns the splits where the caller is organizer or a payer.
func (s *Service) ListMine(ctx context.Context, actorID uuid.UUID) ([]*splitpayment.SplitPayment, error) {
	actor, err := s.users.FindByID(ctx, actorID)
	if err != nil {
		return nil, err
	}
	return s.splits.ListForUser(ctx, actorID, strings.ToLower(actor.Email))
}
