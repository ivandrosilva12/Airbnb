package payment

import (
	"log/slog"
	"strings"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/config"
)

// NewGateway selects a PaymentGateway implementation from configuration:
// "stripe" and "appypay" call the respective real REST APIs; anything else
// (including the default "fake") returns the in-memory FakeGateway so the system
// runs end-to-end without a processor or credentials.
func NewGateway(cfg config.PaymentConfig) port.PaymentGateway {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "stripe":
		if cfg.Stripe.SecretKey == "" {
			slog.Warn("payment: provider=stripe but STRIPE_SECRET_KEY is empty; falling back to fake gateway")
			return NewFakeGateway()
		}
		slog.Info("payment: using Stripe gateway", "baseURL", cfg.Stripe.BaseURL)
		return NewStripeGateway(cfg.Stripe)
	case "appypay":
		if cfg.AppyPay.Token == "" {
			slog.Warn("payment: provider=appypay but APPYPAY_TOKEN is empty; falling back to fake gateway")
			return NewFakeGateway()
		}
		slog.Info("payment: using AppyPay gateway", "baseURL", cfg.AppyPay.BaseURL)
		return NewAppyPayGateway(cfg.AppyPay)
	default:
		slog.Info("payment: using fake gateway (no external processor)")
		return NewFakeGateway()
	}
}
