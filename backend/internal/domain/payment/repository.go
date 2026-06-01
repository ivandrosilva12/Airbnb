package payment

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Revenue is a read-model aggregating payment amounts for reporting.
type Revenue struct {
	CapturedCents int64
	PendingCents  int64 // authorized but not yet captured
	Currency      string
}

// RevenueAccumulator folds payment rows into per-currency totals so the read
// model never sums different currencies into one figure. CapturedCents counts
// the net the host keeps: the full charge for captured payments and the
// non-refunded remainder for refunded ones. dominant() returns the totals for
// the currency with the most money (deterministic tie-break), since the Revenue
// read-model holds a single currency.
type RevenueAccumulator struct {
	byCurrency map[string]*Revenue
}

// NewRevenueAccumulator builds an empty accumulator.
func NewRevenueAccumulator() *RevenueAccumulator {
	return &RevenueAccumulator{byCurrency: map[string]*Revenue{}}
}

// Add folds one payment (its currency, status, original amount and refunded
// amount) into the per-currency totals.
func (a *RevenueAccumulator) Add(currency string, status Status, amountCents, refundedCents int64) {
	rev := a.byCurrency[currency]
	if rev == nil {
		rev = &Revenue{Currency: currency}
		a.byCurrency[currency] = rev
	}
	switch status {
	case StatusCaptured:
		rev.CapturedCents += amountCents
	case StatusAuthorized:
		rev.PendingCents += amountCents
	case StatusRefunded:
		// What the host actually kept after the refund (0 for a full release).
		if kept := amountCents - refundedCents; kept > 0 {
			rev.CapturedCents += kept
		}
	}
}

// Dominant returns the totals for the currency holding the most money. Ties are
// broken by currency code so the result is deterministic.
func (a *RevenueAccumulator) Dominant() Revenue {
	var best Revenue
	var bestKey string
	first := true
	for cur, rev := range a.byCurrency {
		total := rev.CapturedCents + rev.PendingCents
		bestTotal := best.CapturedCents + best.PendingCents
		if first || total > bestTotal || (total == bestTotal && cur < bestKey) {
			best, bestKey, first = *rev, cur, false
		}
	}
	return best
}

// DepositRepository is the persistence port for the DepositHold aggregate.
//
// Like Payment, the aggregate owns its Adjustments slice — Create/Update
// must persist the deposit row plus any new adjustment entries in one
// transaction, and reads must hydrate the slice.
type DepositRepository interface {
	Create(ctx context.Context, d *DepositHold) error
	Update(ctx context.Context, d *DepositHold) error
	FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*DepositHold, error)
	FindByID(ctx context.Context, id uuid.UUID) (*DepositHold, error)
}

// Repository is the persistence port for the Payment aggregate.
//
// Adjustments (partial refunds, damage claims) are owned by the aggregate and
// persisted alongside it: Create and Update must save the Payment row plus
// any new entries in its Adjustments slice in a single transaction; reads
// must hydrate the slice from the same store.
type Repository interface {
	Create(ctx context.Context, p *Payment) error
	Update(ctx context.Context, p *Payment) error
	FindByID(ctx context.Context, id uuid.UUID) (*Payment, error)
	FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*Payment, error)
	// FindByGatewayRef locates a payment by its stored gateway reference (an
	// exact match). Used to reconcile asynchronous gateway webhook events.
	FindByGatewayRef(ctx context.Context, gatewayRef string) (*Payment, error)
	ListByGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*Payment], error)
	// RevenueForBookings totals captured (net of refunds) and pending amounts
	// across the given bookings, never mixing currencies; when bookings span
	// currencies it returns the dominant currency's totals (see
	// RevenueAccumulator).
	RevenueForBookings(ctx context.Context, bookingIDs []uuid.UUID) (Revenue, error)
}
