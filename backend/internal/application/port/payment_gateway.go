package port

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
)

// PaymentGateway abstracts a payment provider (Stripe, Adyen, …). It is an
// outbound port: the application layer depends on it, infrastructure implements
// it. The idempotencyKey lets the gateway de-duplicate retried authorizations.
type PaymentGateway interface {
	Authorize(ctx context.Context, amount shared.Money, idempotencyKey string) (ref string, err error)
	Capture(ctx context.Context, ref string) error
	Refund(ctx context.Context, ref string, amountCents int64) error
}
