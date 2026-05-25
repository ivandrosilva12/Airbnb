package port

import (
	"context"
	"net/http"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Disburser abstracts the payout rail that moves a host's earned balance to
// their connected account (e.g. a Stripe Connect transfer). It is an outbound
// port: the payout use case depends on it, infrastructure implements it.
// destination is the host's payout-account id (a Stripe acct_…). The
// idempotencyKey lets the rail de-duplicate retried payouts so a host is never
// paid twice for the same disbursement.
type Disburser interface {
	Disburse(ctx context.Context, hostID uuid.UUID, destination string, amount shared.Money, idempotencyKey string) (ref string, err error)
}

// ConnectAccount is the state of a host's connected payout account.
type ConnectAccount struct {
	ID             string
	PayoutsEnabled bool // true once onboarding is complete and payouts can be sent
}

// ConnectGateway abstracts the payout-account onboarding rail (Stripe Connect):
// creating a connected account for a host, generating the hosted onboarding
// link they complete, and reading back whether the account can receive payouts.
// Outbound port; infrastructure implements the provider calls.
type ConnectGateway interface {
	CreateAccount(ctx context.Context, email string) (ConnectAccount, error)
	CreateOnboardingLink(ctx context.Context, accountID, refreshURL, returnURL string) (url string, err error)
	GetAccount(ctx context.Context, accountID string) (ConnectAccount, error)
}

// ConnectAccountEvent is a normalized connected-account webhook (e.g. Stripe's
// account.updated), carrying the account id and its current payout capability.
type ConnectAccountEvent struct {
	EventID        string // provider delivery id (evt_…), for storage-level dedup
	AccountID      string // the connected account (acct_…)
	PayoutsEnabled bool
}

// ConnectWebhookVerifier authenticates and parses an inbound Connect webhook
// into a ConnectAccountEvent. Inbound port; infrastructure implements the
// provider-specific signature check and payload mapping. ok=false means the
// request was authentic but is not an account-state event we act on.
type ConnectWebhookVerifier interface {
	Verify(header http.Header, body []byte) (event ConnectAccountEvent, ok bool, err error)
}
