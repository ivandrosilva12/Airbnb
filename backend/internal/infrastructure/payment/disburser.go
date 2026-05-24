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

// NewDisburser selects a payout rail from configuration. A Stripe transfer rail
// is used when provider=stripe and both a secret key and a destination Connect
// account are configured; otherwise the fake rail runs so the system works
// end-to-end without a processor.
func NewDisburser(cfg config.PaymentConfig) port.Disburser {
	if strings.EqualFold(strings.TrimSpace(cfg.Provider), nameStripe) &&
		cfg.Stripe.SecretKey != "" && cfg.Stripe.ConnectAccount != "" {
		slog.Info("payout: using Stripe transfer rail", "destination", cfg.Stripe.ConnectAccount)
		return NewStripeDisburser(cfg.Stripe)
	}
	slog.Info("payout: using fake disbursement rail (no external payout processor)")
	return NewFakeDisburser()
}

// FakeDisburser is an in-memory payout rail that always succeeds, returning a
// synthetic reference. It lets the disbursement workflow run end-to-end without
// a real processor.
type FakeDisburser struct{}

// NewFakeDisburser builds a FakeDisburser.
func NewFakeDisburser() *FakeDisburser { return &FakeDisburser{} }

// Disburse pretends to pay out and returns a synthetic transfer reference.
func (d *FakeDisburser) Disburse(_ context.Context, _ uuid.UUID, _ shared.Money, _ string) (string, error) {
	return "fake_tr_" + uuid.NewString(), nil
}

// StripeDisburser pays hosts via the Stripe transfers API. Transfers credit the
// configured connected account; per-host routing arrives with Connect
// onboarding (each host gets their own acct_… stored on the profile).
type StripeDisburser struct {
	gw          *StripeGateway
	destination string
}

// NewStripeDisburser builds a StripeDisburser from config, reusing the Stripe
// gateway's authenticated transport.
func NewStripeDisburser(cfg config.StripeConfig) *StripeDisburser {
	return &StripeDisburser{gw: NewStripeGateway(cfg), destination: cfg.ConnectAccount}
}

// Disburse creates a Stripe transfer to the destination connected account. The
// idempotency key prevents a retried payout from transferring twice.
func (d *StripeDisburser) Disburse(ctx context.Context, hostID uuid.UUID, amount shared.Money, idempotencyKey string) (string, error) {
	form := url.Values{}
	form.Set("amount", strconv.FormatInt(amount.AmountCents(), 10))
	form.Set("currency", strings.ToLower(amount.Currency()))
	form.Set("destination", d.destination)
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
