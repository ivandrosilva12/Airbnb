package port

import (
	"context"
	"net/http"
	"time"

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

// GatewayEventType is the normalized kind of an inbound webhook event, mapped
// from each provider's own event vocabulary.
type GatewayEventType string

const (
	// GatewayAuthorized: the gateway finished an asynchronous authorization
	// (e.g. Stripe 3DS challenge cleared, AppyPay redirect completed). The
	// initial Authorize call returned a pending reference; this webhook is
	// the gateway telling us the hold is now in place. The reconciler moves
	// a still-pending local payment to authorized and emits PaymentAuthorized
	// so the booking context can auto-confirm if appropriate.
	GatewayAuthorized GatewayEventType = "authorized"
	// GatewayCaptured: funds were captured/settled.
	GatewayCaptured GatewayEventType = "captured"
	// GatewayRefunded: funds were returned to the payer.
	GatewayRefunded GatewayEventType = "refunded"
	// GatewayFailed: the payment failed or was declined.
	GatewayFailed GatewayEventType = "failed"
	// GatewayIgnored: a valid event we take no action on.
	GatewayIgnored GatewayEventType = "ignored"
)

// GatewayEvent is a provider-agnostic webhook event used to reconcile a local
// payment with the gateway's authoritative state.
type GatewayEvent struct {
	Provider      string
	EventID       string // provider-native delivery id, for storage-level dedup (may be empty)
	Reference     string // provider-native payment reference (e.g. pi_… / chg_…)
	Type          GatewayEventType
	AmountCents   int64 // refund amount; 0 means "full"
	FailureReason string
}

// WebhookDedupeStore records processed webhook deliveries so retried or
// duplicate deliveries can be skipped at storage level (the reconciliation is
// already idempotent; this avoids redundant work and gives an audit trail).
type WebhookDedupeStore interface {
	// Seen reports whether (provider, eventID) was already recorded.
	Seen(ctx context.Context, provider, eventID string) (bool, error)
	// Record persists (provider, eventID); it is idempotent.
	Record(ctx context.Context, provider, eventID string) error
	// DeleteOlderThan removes records created before cutoff and returns how many
	// were deleted — a retention/cleanup operation.
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// WebhookVerifier authenticates and parses a provider's webhook request into a
// normalized GatewayEvent. It is an inbound port: infrastructure implements the
// provider-specific signature check and payload mapping. ok=false means the
// request was authentic but carries no actionable event.
type WebhookVerifier interface {
	Verify(header http.Header, body []byte) (event GatewayEvent, ok bool, err error)
}
