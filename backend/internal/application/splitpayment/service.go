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
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// Service orchestrates split-payment use cases.
//
// AuthorizeShare runs inside a UnitOfWork so the share-paid write and the
// SplitPaymentCompleted outbox append commit atomically — S31. A crash
// between the DB write and the event being recorded would otherwise strand
// the booking in pending forever; the outbox recovery relay re-delivers any
// completion event whose dispatch was interrupted, and the in-tx append
// guarantees the event is recorded if and only if the share is marked paid.
type Service struct {
	splits splitpayment.Repository
	users  user.Repository
	uow    port.UnitOfWork
}

// NewService wires the split-payment application service. The UnitOfWork is
// used by AuthorizeShare so the share-paid write and the
// SplitPaymentCompleted outbox append commit atomically. uow may be nil in
// unit tests that don't exercise the completion path.
func NewService(splits splitpayment.Repository, users user.Repository, uow port.UnitOfWork) *Service {
	return &Service{splits: splits, users: users, uow: uow}
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
	if s.uow == nil {
		// No UoW wired — fall back to the non-durable path. Used by tests
		// that don't exercise completion; production wires a real UoW.
		return s.authorizeWithoutUoW(ctx, actorID, splitID, shareID, actor.Email)
	}
	var out *splitpayment.SplitPayment
	err = s.uow.Run(ctx, func(tx port.Tx) error {
		sp, err := tx.SplitPayments.FindByID(ctx, splitID)
		if err != nil {
			return err
		}
		if err := sp.MarkSharePaid(shareID, actorID, actor.Email); err != nil {
			return err
		}
		completed := false
		if sp.AllPaid() {
			if err := sp.MarkCompleted(); err != nil {
				return err
			}
			completed = true
		}
		if err := tx.SplitPayments.Update(ctx, sp); err != nil {
			return err
		}
		// S31 — append the completion event inside the same transaction as
		// the share-paid write. The outbox row commits atomically with the
		// domain change, so a crash between the two cannot lose the event;
		// the recovery relay picks up any record whose dispatch was
		// interrupted on the next startup.
		if completed {
			rec, err := event.NewRecord(event.SplitPaymentCompleted{
				SplitPaymentID: sp.ID,
				BookingID:      sp.BookingID,
			})
			if err != nil {
				return err
			}
			if err := tx.Outbox.Append(ctx, rec); err != nil {
				return err
			}
		}
		out = sp
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// authorizeWithoutUoW is the legacy path retained for tests that construct
// the service without a UnitOfWork. It does NOT publish the completion event
// — callers that need the booking confirmation must use the UoW path.
func (s *Service) authorizeWithoutUoW(ctx context.Context, actorID, splitID, shareID uuid.UUID, actorEmail string) (*splitpayment.SplitPayment, error) {
	sp, err := s.splits.FindByID(ctx, splitID)
	if err != nil {
		return nil, err
	}
	if err := sp.MarkSharePaid(shareID, actorID, actorEmail); err != nil {
		return nil, err
	}
	if sp.AllPaid() {
		if err := sp.MarkCompleted(); err != nil {
			return nil, err
		}
	}
	if err := s.splits.Update(ctx, sp); err != nil {
		return nil, err
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
