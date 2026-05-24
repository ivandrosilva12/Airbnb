package payout

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for host-earnings ledger entries.
type Repository interface {
	Create(ctx context.Context, e *Entry) error
	// ListByHost returns a host's ledger entries, newest first.
	ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*Entry], error)
	// HasEarningForBooking reports whether an earning has already been recorded
	// for a booking (so a cancellation only reverses realised earnings).
	HasEarningForBooking(ctx context.Context, bookingID uuid.UUID) (bool, error)
	// BalancesByHost aggregates the host's ledger into one Balance per currency.
	BalancesByHost(ctx context.Context, hostID uuid.UUID) ([]Balance, error)

	// CreateDisbursement persists a new payout record.
	CreateDisbursement(ctx context.Context, d *Disbursement) error
	// UpdateDisbursement persists status/reference changes to a payout.
	UpdateDisbursement(ctx context.Context, d *Disbursement) error
	// ListDisbursementsByHost returns a host's payouts, newest first.
	ListDisbursementsByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*Disbursement], error)
	// DisbursedCentsByHost sums committed (pending+paid) payouts per currency, so
	// the available balance can subtract funds already paid out or in flight.
	DisbursedCentsByHost(ctx context.Context, hostID uuid.UUID) (map[string]int64, error)
}
