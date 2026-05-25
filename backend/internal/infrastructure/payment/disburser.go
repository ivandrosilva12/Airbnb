package payment

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// NewDisburser selects a payout rail from configuration. When provider=stripe
// and a secret key is set, payouts are real Stripe Connect transfers to each
// host's connected account; otherwise the fake rail runs so the system works
// end-to-end without a processor.
func NewDisburser(cfg config.PaymentConfig) port.Disburser {
	if strings.EqualFold(strings.TrimSpace(cfg.Provider), nameStripe) && cfg.Stripe.SecretKey != "" {
		slog.Info("payout: using Stripe Connect transfer rail")
		return NewStripeDisburser(cfg.Stripe)
	}
	slog.Info("payout: using fake disbursement rail (no external payout processor)")
	return NewFakeDisburser()
}

// NewConnectGateway selects the payout-account onboarding rail. Real Stripe
// Connect when provider=stripe with a secret key; otherwise a fake gateway that
// returns synthetic, immediately-enabled accounts so onboarding works locally.
func NewConnectGateway(cfg config.PaymentConfig) port.ConnectGateway {
	if strings.EqualFold(strings.TrimSpace(cfg.Provider), nameStripe) && cfg.Stripe.SecretKey != "" {
		slog.Info("payout: using Stripe Connect onboarding", "country", cfg.Stripe.ConnectCountry)
		return NewStripeConnectGateway(cfg.Stripe)
	}
	slog.Info("payout: using fake Connect onboarding")
	return NewFakeConnectGateway()
}

// --- Fake rail ---------------------------------------------------------------

// FakeDisburser is an in-memory payout rail that always succeeds, returning a
// synthetic reference. It lets the disbursement workflow run end-to-end without
// a real processor.
type FakeDisburser struct{}

// NewFakeDisburser builds a FakeDisburser.
func NewFakeDisburser() *FakeDisburser { return &FakeDisburser{} }

// Disburse pretends to pay out and returns a synthetic transfer reference.
func (d *FakeDisburser) Disburse(_ context.Context, _ uuid.UUID, _ string, _ shared.Money, _ string) (string, error) {
	return "fake_tr_" + uuid.NewString(), nil
}

// FakeConnectGateway mints synthetic, immediately-payouts-enabled accounts and
// a local onboarding URL, so the host payout flow is exercisable without Stripe.
type FakeConnectGateway struct{}

// NewFakeConnectGateway builds a FakeConnectGateway.
func NewFakeConnectGateway() *FakeConnectGateway { return &FakeConnectGateway{} }

func (g *FakeConnectGateway) CreateAccount(_ context.Context, _ string) (port.ConnectAccount, error) {
	return port.ConnectAccount{ID: "fake_acct_" + uuid.NewString(), PayoutsEnabled: true}, nil
}

func (g *FakeConnectGateway) CreateOnboardingLink(_ context.Context, accountID, _, returnURL string) (string, error) {
	// Point straight back at the return URL: the fake account is already enabled,
	// so there is nothing to fill in.
	if returnURL == "" {
		return "https://connect.fake.invalid/onboard/" + accountID, nil
	}
	return returnURL, nil
}

func (g *FakeConnectGateway) GetAccount(_ context.Context, accountID string) (port.ConnectAccount, error) {
	return port.ConnectAccount{ID: accountID, PayoutsEnabled: true}, nil
}

// --- Stripe Connect rail -----------------------------------------------------

// StripeDisburser pays hosts via the Stripe transfers API, crediting the host's
// own connected account (the destination passed per call).
type StripeDisburser struct {
	gw *StripeGateway
}

// NewStripeDisburser builds a StripeDisburser, reusing the Stripe gateway's
// authenticated transport.
func NewStripeDisburser(cfg config.StripeConfig) *StripeDisburser {
	return &StripeDisburser{gw: NewStripeGateway(cfg)}
}

// Disburse creates a Stripe transfer to the host's connected account. The
// idempotency key prevents a retried payout from transferring twice.
func (d *StripeDisburser) Disburse(ctx context.Context, hostID uuid.UUID, destination string, amount shared.Money, idempotencyKey string) (string, error) {
	if strings.TrimSpace(destination) == "" {
		return "", fmt.Errorf("stripe: missing destination connected account")
	}
	form := url.Values{}
	form.Set("amount", strconv.FormatInt(amount.AmountCents(), 10))
	form.Set("currency", strings.ToLower(amount.Currency()))
	form.Set("destination", destination)
	form.Set("transfer_group", "host_"+hostID.String())

	var out struct {
		ID string `json:"id"`
	}
	if err := d.gw.do(ctx, "/v1/transfers", form, idempotencyKey, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("stripe: empty transfer id")
	}
	return out.ID, nil
}

// StripeConnectGateway onboards hosts as Stripe Connect Express accounts and
// reads back their payout capability.
type StripeConnectGateway struct {
	gw      *StripeGateway
	country string
}

// NewStripeConnectGateway builds a StripeConnectGateway from config.
func NewStripeConnectGateway(cfg config.StripeConfig) *StripeConnectGateway {
	country := strings.TrimSpace(cfg.ConnectCountry)
	if country == "" {
		country = "US"
	}
	return &StripeConnectGateway{gw: NewStripeGateway(cfg), country: country}
}

type stripeAccount struct {
	ID             string `json:"id"`
	PayoutsEnabled bool   `json:"payouts_enabled"`
}

// CreateAccount provisions an Express connected account with the transfers
// capability requested, so the platform can pay the host out.
func (g *StripeConnectGateway) CreateAccount(ctx context.Context, email string) (port.ConnectAccount, error) {
	form := url.Values{}
	form.Set("type", "express")
	form.Set("country", g.country)
	if email != "" {
		form.Set("email", email)
	}
	form.Set("capabilities[transfers][requested]", "true")

	var out stripeAccount
	if err := g.gw.do(ctx, "/v1/accounts", form, "", &out); err != nil {
		return port.ConnectAccount{}, err
	}
	if out.ID == "" {
		return port.ConnectAccount{}, fmt.Errorf("stripe: empty account id")
	}
	return port.ConnectAccount{ID: out.ID, PayoutsEnabled: out.PayoutsEnabled}, nil
}

// CreateOnboardingLink returns a hosted Stripe onboarding URL for the account.
func (g *StripeConnectGateway) CreateOnboardingLink(ctx context.Context, accountID, refreshURL, returnURL string) (string, error) {
	form := url.Values{}
	form.Set("account", accountID)
	form.Set("type", "account_onboarding")
	if refreshURL != "" {
		form.Set("refresh_url", refreshURL)
	}
	if returnURL != "" {
		form.Set("return_url", returnURL)
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := g.gw.do(ctx, "/v1/account_links", form, "", &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("stripe: empty onboarding url")
	}
	return out.URL, nil
}

// GetAccount reads back the account's current payout capability.
func (g *StripeConnectGateway) GetAccount(ctx context.Context, accountID string) (port.ConnectAccount, error) {
	var out stripeAccount
	if err := g.gw.get(ctx, "/v1/accounts/"+url.PathEscape(accountID), &out); err != nil {
		return port.ConnectAccount{}, err
	}
	return port.ConnectAccount{ID: out.ID, PayoutsEnabled: out.PayoutsEnabled}, nil
}
