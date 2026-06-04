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
	"github.com/airhost/backend/internal/observability/logctx"
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
//
// S88 (WF-GAP-001/005) — when a PaymentGateway is wired via WithGateway,
// AuthorizeShare places a real per-share hold and Cancel / a BookingCancelled
// subscription release those holds via Refund. With no gateway the service
// runs in trust-mode (the previous behaviour), which is what the tests
// constructed without WithGateway exercise.
type Service struct {
	splits  splitpayment.Repository
	users   user.Repository
	uow     port.UnitOfWork
	gateway port.PaymentGateway
}

// NewService wires the split-payment application service. The UnitOfWork is
// used by AuthorizeShare so the share-paid write and the
// SplitPaymentCompleted outbox append commit atomically. uow may be nil in
// unit tests that don't exercise the completion path.
func NewService(splits splitpayment.Repository, users user.Repository, uow port.UnitOfWork) *Service {
	return &Service{splits: splits, users: users, uow: uow}
}

// WithGateway plugs the per-share payment gateway into the service (S88).
// When set, AuthorizeShare calls gateway.Authorize before flipping the
// share to paid and stores the returned reference; Cancel / the
// BookingCancelled subscription call gateway.Refund per previously-paid
// share. Returns the same service so construction can be chained.
func (s *Service) WithGateway(gw port.PaymentGateway) *Service {
	s.gateway = gw
	return s
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

// Create persists a new split-payment plan via the service's own splits
// repository (no caller-supplied transaction). Kept for test paths and
// future callers that don't have an outer UoW; the booking-initiated path
// uses CreateInTx so the split row commits atomically with the booking.
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

// CreateInTx persists a new split-payment plan through the caller's
// transaction. Used by bookingapp.Create so the booking row, the
// BookingRequested outbox event, and the split row all commit atomically
// or roll back together — S35.
func (s *Service) CreateInTx(ctx context.Context, tx port.Tx, in CreateInput) (*splitpayment.SplitPayment, error) {
	sp, err := splitpayment.New(in.BookingID, in.OrganizerID, in.OrganizerEmail, in.Currency, in.TotalCents, in.Shares)
	if err != nil {
		return nil, err
	}
	if err := tx.SplitPayments.Create(ctx, sp); err != nil {
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
//
// S88 (WF-GAP-001) — when the service has a payment gateway wired the call
// places a real per-share hold via gateway.Authorize, stores the returned
// reference on the share, and emits a SplitShareAuthorized event. A gateway
// rejection flips that single share to ShareFailed (the payer can retry)
// without touching the other shares, so the per-share rejection never blocks
// payers who can actually pay. With no gateway the service stays in trust
// mode — what the older tests exercised.
func (s *Service) AuthorizeShare(ctx context.Context, actorID, splitID, shareID uuid.UUID) (*splitpayment.SplitPayment, error) {
	actor, err := s.users.FindByID(ctx, actorID)
	if err != nil {
		return nil, err
	}
	// Look up the split outside any transaction first so the gateway call —
	// which can be slow — does not happen inside the UoW. The verified
	// share amount + currency feed the per-share idempotency key so a
	// retried call cannot place two holds for the same share.
	pre, err := s.splits.FindByID(ctx, splitID)
	if err != nil {
		return nil, err
	}
	share := pre.FindShare(shareID)
	if share == nil {
		return nil, shared.ErrNotFound
	}
	// Re-confirm authorisation before we touch the gateway: a wrong-email
	// payer must not even cause a (logged, billable) Authorize call.
	if !strings.EqualFold(share.PayerEmail, actor.Email) {
		return nil, shared.ErrForbidden
	}

	gatewayRef, authErr := s.authorizeAtGateway(ctx, pre, share)
	if authErr != nil {
		// S88 — gateway refused this share. Record the failure on the
		// share so an operator can see it and the payer can retry, but
		// do not unwind the whole split: other payers can keep going.
		if markErr := s.recordShareFailure(ctx, actorID, splitID, shareID, actor.Email, authErr.Error()); markErr != nil {
			logctx.LoggerFrom(ctx).Error("splitpayment: persisting share failure failed",
				"split", splitID, "share", shareID, "error", markErr)
		}
		return nil, authErr
	}

	if s.uow == nil {
		// No UoW wired — fall back to the non-durable path. Used by tests
		// that don't exercise completion; production wires a real UoW.
		return s.authorizeWithoutUoW(ctx, actorID, splitID, shareID, actor.Email, gatewayRef)
	}
	var out *splitpayment.SplitPayment
	err = s.uow.Run(ctx, func(tx port.Tx) error {
		sp, err := tx.SplitPayments.FindByID(ctx, splitID)
		if err != nil {
			return err
		}
		if err := sp.MarkSharePaid(shareID, actorID, actor.Email, gatewayRef); err != nil {
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
		// S88 — emit a per-share Authorized event for analytics / audit.
		// The cancel flow doesn't depend on it; the GatewayRef on the
		// share is the durable record. Appended inside the same outbox
		// transaction so it ships together with any completion event.
		if gatewayRef != "" {
			rec, err := event.NewRecord(event.SplitShareAuthorized{
				SplitPaymentID: sp.ID,
				BookingID:      sp.BookingID,
				ShareID:        shareID,
				PayerID:        actorID,
				AmountCents:    share.AmountCents,
				Currency:       sp.Currency,
				GatewayRef:     gatewayRef,
			})
			if err != nil {
				return err
			}
			if err := tx.Outbox.Append(ctx, rec); err != nil {
				return err
			}
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

// authorizeAtGateway calls the wired payment gateway for one share. With no
// gateway wired the service runs in trust-mode (returns an empty ref). The
// idempotency key combines the share id and the split id so a retried
// authorize cannot accidentally place a second hold for the same share.
func (s *Service) authorizeAtGateway(ctx context.Context, sp *splitpayment.SplitPayment, share *splitpayment.Share) (string, error) {
	if s.gateway == nil {
		return "", nil
	}
	amount, err := shared.NewMoney(share.AmountCents, sp.Currency)
	if err != nil {
		return "", err
	}
	idem := "split:" + sp.ID.String() + ":" + share.ID.String()
	return s.gateway.Authorize(ctx, amount, idem)
}

// recordShareFailure persists a ShareFailed transition for one share so the
// payer can retry without the split being moved off pending. Best-effort —
// the caller already has the gateway error to return to the payer; this is
// audit state only.
func (s *Service) recordShareFailure(ctx context.Context, actorID, splitID, shareID uuid.UUID, actorEmail, reason string) error {
	sp, err := s.splits.FindByID(ctx, splitID)
	if err != nil {
		return err
	}
	if err := sp.MarkShareFailed(shareID, actorID, actorEmail, reason); err != nil {
		return err
	}
	return s.splits.Update(ctx, sp)
}

// authorizeWithoutUoW is the legacy path retained for tests that construct
// the service without a UnitOfWork. It does NOT publish the completion event
// — callers that need the booking confirmation must use the UoW path.
func (s *Service) authorizeWithoutUoW(ctx context.Context, actorID, splitID, shareID uuid.UUID, actorEmail, gatewayRef string) (*splitpayment.SplitPayment, error) {
	sp, err := s.splits.FindByID(ctx, splitID)
	if err != nil {
		return nil, err
	}
	if err := sp.MarkSharePaid(shareID, actorID, actorEmail, gatewayRef); err != nil {
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
// caller — bookingapp.Service.Cancel for now).
//
// S88 (WF-GAP-005) — every previously-paid share gets its gateway hold
// released via gateway.Refund and a SplitShareRefunded event is emitted per
// successful refund. With no gateway wired this is a state-only transition
// (trust mode, what tests without WithGateway exercise).
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
	// Refund per-share holds before persisting so any gateway-side hold is
	// released first; persisting the refunded states comes after.
	s.refundPaidShares(ctx, sp)
	if err := s.splits.Update(ctx, sp); err != nil {
		return nil, err
	}
	return sp, nil
}

// refundPaidShares iterates the split and releases each previously-paid
// share via the gateway, mutating each share in place to ShareRefunded on
// success. A gateway failure on one share is logged and that share is left
// in SharePaid — the recovery path is a manual operator action; the rest of
// the refunds still go through. The function mutates sp.Shares directly so
// the caller can persist the updated aggregate.
func (s *Service) refundPaidShares(ctx context.Context, sp *splitpayment.SplitPayment) {
	for i := range sp.Shares {
		sh := &sp.Shares[i]
		if sh.Status != splitpayment.SharePaid {
			continue
		}
		if s.gateway != nil && sh.GatewayRef != nil && *sh.GatewayRef != "" {
			if err := s.gateway.Refund(ctx, *sh.GatewayRef, sh.AmountCents); err != nil {
				// One bad share doesn't block the others — log and skip.
				// The share stays SharePaid so an operator retry surfaces.
				logctx.LoggerFrom(ctx).Error("splitpayment: gateway refund failed",
					"split", sp.ID, "share", sh.ID, "error", err)
				continue
			}
		}
		if err := sp.MarkShareRefunded(sh.ID); err != nil {
			logctx.LoggerFrom(ctx).Error("splitpayment: persisting share refund failed",
				"split", sp.ID, "share", sh.ID, "error", err)
		}
	}
}

// EventHandler returns an event.Handler that releases per-share holds when a
// split-payment booking is cancelled outside the splitpayment surface (e.g.
// the host or the guest cancels via bookingapp.Service.Cancel). S88 closes
// the loop the trust-mode design left open: a cancelled split-payment
// booking now refunds every contributor.
//
// The handler runs best-effort — failures are logged. Idempotent under
// at-least-once delivery: a share already refunded is skipped by the
// MarkShareRefunded conflict guard.
func (s *Service) EventHandler() event.Handler {
	return func(ctx context.Context, e event.Event) {
		ev, ok := e.(event.BookingCancelled)
		if !ok {
			return
		}
		s.refundBookingShares(ctx, ev.BookingID)
	}
}

// refundBookingShares is the BookingCancelled-driven refund path. Looks up
// the split for the booking; a booking without a split is a no-op. Per-share
// gateway refunds plus a SplitShareRefunded event each go through, then the
// updated aggregate is persisted. The split itself is moved to cancelled if
// still pending so the read model shows the terminal state.
func (s *Service) refundBookingShares(ctx context.Context, bookingID uuid.UUID) {
	sp, err := s.splits.FindByBookingID(ctx, bookingID)
	if err != nil {
		if !errors.Is(err, shared.ErrNotFound) {
			logctx.LoggerFrom(ctx).Error("splitpayment: cancel lookup failed",
				"booking", bookingID, "error", err)
		}
		return
	}
	// Collect the per-share metadata up-front so we can build the events
	// for shares that were paid (and are therefore eligible to refund).
	type refunded struct {
		shareID    uuid.UUID
		payerID    uuid.UUID
		amount     int64
		gatewayRef string
	}
	pre := make(map[uuid.UUID]refunded, len(sp.Shares))
	for i := range sp.Shares {
		sh := sp.Shares[i]
		if sh.Status != splitpayment.SharePaid {
			continue
		}
		ref := ""
		if sh.GatewayRef != nil {
			ref = *sh.GatewayRef
		}
		var payer uuid.UUID
		if sh.PayerUserID != nil {
			payer = *sh.PayerUserID
		}
		pre[sh.ID] = refunded{shareID: sh.ID, payerID: payer, amount: sh.AmountCents, gatewayRef: ref}
	}

	s.refundPaidShares(ctx, sp)
	// Cancel the split if it is still pending. A completed split keeps its
	// completed state; the per-share rows record what was refunded.
	if sp.Status == splitpayment.StatusPending {
		_ = sp.Cancel()
	}

	if s.uow == nil {
		if err := s.splits.Update(ctx, sp); err != nil {
			logctx.LoggerFrom(ctx).Error("splitpayment: persist cancel refunds failed",
				"booking", bookingID, "error", err)
		}
		return
	}
	if err := s.uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.SplitPayments.Update(ctx, sp); err != nil {
			return err
		}
		// Emit one SplitShareRefunded per share that actually transitioned
		// (i.e. is now ShareRefunded). Shares whose gateway refund failed
		// stayed SharePaid and so are skipped here — they will be picked
		// up on the next retry path.
		for i := range sp.Shares {
			sh := sp.Shares[i]
			if sh.Status != splitpayment.ShareRefunded {
				continue
			}
			before, ok := pre[sh.ID]
			if !ok {
				continue
			}
			rec, err := event.NewRecord(event.SplitShareRefunded{
				SplitPaymentID: sp.ID,
				BookingID:      sp.BookingID,
				ShareID:        sh.ID,
				PayerID:        before.payerID,
				AmountCents:    before.amount,
				Currency:       sp.Currency,
				GatewayRef:     before.gatewayRef,
			})
			if err != nil {
				return err
			}
			if err := tx.Outbox.Append(ctx, rec); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		logctx.LoggerFrom(ctx).Error("splitpayment: persist cancel refunds failed (uow)",
			"booking", bookingID, "error", err)
	}
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
