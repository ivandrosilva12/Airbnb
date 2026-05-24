package port

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Disburser abstracts the payout rail that moves a host's earned balance to
// their external account (e.g. a Stripe Connect transfer or a bank payout). It
// is an outbound port: the payout use case depends on it, infrastructure
// implements it. The idempotencyKey lets the rail de-duplicate retried payouts
// so a host is never paid twice for the same disbursement.
type Disburser interface {
	Disburse(ctx context.Context, hostID uuid.UUID, amount shared.Money, idempotencyKey string) (ref string, err error)
}
