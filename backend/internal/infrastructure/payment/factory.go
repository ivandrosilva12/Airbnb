package payment

import (
	"log/slog"
	"strings"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/config"
)

// Gateway name tags used by the routing layer and persisted in references.
const (
	nameStripe     = "stripe"
	nameAppyPay    = "appypay"
	nameGPayAngola = "gpayangola"
	nameFake       = "fake"
)

// NewGateway selects a PaymentGateway implementation from configuration:
//
//   - "stripe" / "appypay" / "gpayangola" force a single real gateway (falling
//     back to the fake one when the credential is unset).
//   - "auto" routes by currency: domestic payments (DomesticCurrency, e.g. AOA)
//     go to GPay Angola with AppyPay failover; foreign payments go to Stripe.
//     Each leg falls back to the fake gateway when nothing is configured, so the
//     system always runs.
//   - anything else (including the default "fake") returns the in-memory
//     FakeGateway so the system runs end-to-end without a processor.
func NewGateway(cfg config.PaymentConfig) port.PaymentGateway {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case nameStripe:
		if cfg.Stripe.SecretKey == "" {
			slog.Warn("payment: provider=stripe but STRIPE_SECRET_KEY is empty; falling back to fake gateway")
			return NewFakeGateway()
		}
		slog.Info("payment: using Stripe gateway", "baseURL", cfg.Stripe.BaseURL)
		return NewStripeGateway(cfg.Stripe)
	case nameAppyPay:
		if cfg.AppyPay.Token == "" {
			slog.Warn("payment: provider=appypay but APPYPAY_TOKEN is empty; falling back to fake gateway")
			return NewFakeGateway()
		}
		slog.Info("payment: using AppyPay gateway", "baseURL", cfg.AppyPay.BaseURL)
		return NewAppyPayGateway(cfg.AppyPay)
	case nameGPayAngola:
		if cfg.GPayAngola.APIKey == "" {
			slog.Warn("payment: provider=gpayangola but GPAYANGOLA_API_KEY is empty; falling back to fake gateway")
			return NewFakeGateway()
		}
		slog.Info("payment: using GPay Angola gateway", "baseURL", cfg.GPayAngola.BaseURL)
		return NewGPayAngolaGateway(cfg.GPayAngola)
	case "auto", "router":
		return newRoutingGateway(cfg)
	default:
		slog.Info("payment: using fake gateway (no external processor)")
		return NewFakeGateway()
	}
}

// newRoutingGateway assembles the currency-routing gateway from whichever real
// providers are configured, with the fake gateway backstopping each empty leg.
func newRoutingGateway(cfg config.PaymentConfig) port.PaymentGateway {
	b := newRoutingBuilder(cfg.DomesticCurrency)

	// Domestic chain: GPay Angola (primary) → AppyPay (failover).
	if cfg.GPayAngola.APIKey != "" {
		b.addDomestic(nameGPayAngola, NewGPayAngolaGateway(cfg.GPayAngola))
	}
	if cfg.AppyPay.Token != "" {
		b.addDomestic(nameAppyPay, NewAppyPayGateway(cfg.AppyPay))
	}
	if len(b.domestic) == 0 {
		b.addDomestic(nameFake, NewFakeGateway())
	}

	// Foreign chain: Stripe.
	if cfg.Stripe.SecretKey != "" {
		b.addForeign(nameStripe, NewStripeGateway(cfg.Stripe))
	}
	if len(b.foreign) == 0 {
		b.addForeign(nameFake, NewFakeGateway())
	}

	slog.Info("payment: using currency-routing gateway",
		"domesticCurrency", b.domesticCurrency,
		"domestic", names(b.domestic), "foreign", names(b.foreign))
	return b.build()
}

func names(chain []taggedGateway) []string {
	out := make([]string, 0, len(chain))
	for _, t := range chain {
		out = append(out, t.name)
	}
	return out
}
