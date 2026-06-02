package config

import (
	"errors"
	"fmt"
	"strings"
)

// productionEnvName is the App.Environment value at which Validate
// flips from "warn-only" to "refuse to start". Anything else (dev,
// test, staging-with-real-secrets-but-not-prod) gets warnings only.
const productionEnvName = "production"

// Validate scans the loaded config for placeholder / weak secret
// values (S50). Behaviour depends on App.Environment:
//
//   - production: every issue is an error, Validate returns a joined
//     error so the process refuses to start. A real deployment with
//     "minioadmin" credentials or an empty webhook secret is a security
//     incident waiting to happen.
//   - anything else: every issue is collected into Warnings but
//     Validate returns nil. Local dev keeps working with the defaults.
//
// The check is intentionally conservative — it flags only well-known
// placeholders, not low-entropy strings in general. A regex-based
// entropy check would over-trigger in dev and is the operator's job
// to enforce via real password policy, not the app's.
type ValidationResult struct {
	Warnings []string
}

// Validate runs every check. Returns (nil, ValidationResult) in
// non-production with any number of warnings; in production, returns
// a non-nil error joining every blocker.
func (c *Config) Validate() (ValidationResult, error) {
	res := ValidationResult{}
	add := func(msg string) { res.Warnings = append(res.Warnings, msg) }

	prod := strings.EqualFold(c.App.Environment, productionEnvName)

	// --- Database / storage ---------------------------------------------------
	if c.Database.Password == "" || isPlaceholder(c.Database.Password) {
		add(fmt.Sprintf("DB_PASSWORD is empty or a placeholder (%q) — rotate before production", c.Database.Password))
	}
	if c.Storage.AccessKey == "minioadmin" || c.Storage.SecretKey == "minioadmin" {
		add("MINIO_ACCESS_KEY / MINIO_SECRET_KEY use the default 'minioadmin' — rotate before production")
	}

	// --- Payment providers ----------------------------------------------------
	// When the real (non-fake) gateway is wired, ALL its secrets must be
	// non-empty. A live deployment with an empty webhook secret means
	// signatures pass with the empty key — a silent compromise.
	switch strings.ToLower(c.Payment.Provider) {
	case "stripe":
		requireNonEmpty(&res, "STRIPE_SECRET_KEY", c.Payment.Stripe.SecretKey)
		requireNonEmpty(&res, "STRIPE_WEBHOOK_SECRET", c.Payment.Stripe.WebhookSecret)
	case "appypay":
		requireNonEmpty(&res, "APPYPAY_TOKEN", c.Payment.AppyPay.Token)
		requireNonEmpty(&res, "APPYPAY_WEBHOOK_SECRET", c.Payment.AppyPay.WebhookSecret)
	case "gpayangola":
		requireNonEmpty(&res, "GPAYANGOLA_API_KEY", c.Payment.GPayAngola.APIKey)
		requireNonEmpty(&res, "GPAYANGOLA_WEBHOOK_SECRET", c.Payment.GPayAngola.WebhookSecret)
	case "auto":
		// auto routes domestic → GPay Angola (+ AppyPay failover), foreign → Stripe.
		// Every gateway must therefore be fully configured.
		requireNonEmpty(&res, "STRIPE_SECRET_KEY", c.Payment.Stripe.SecretKey)
		requireNonEmpty(&res, "STRIPE_WEBHOOK_SECRET", c.Payment.Stripe.WebhookSecret)
		requireNonEmpty(&res, "GPAYANGOLA_API_KEY", c.Payment.GPayAngola.APIKey)
		requireNonEmpty(&res, "GPAYANGOLA_WEBHOOK_SECRET", c.Payment.GPayAngola.WebhookSecret)
		requireNonEmpty(&res, "APPYPAY_WEBHOOK_SECRET", c.Payment.AppyPay.WebhookSecret)
	case "fake", "":
		if prod {
			add("PAYMENT_PROVIDER is 'fake' in production — real payments will not be charged")
		}
	}

	// --- Alerting -------------------------------------------------------------
	// The alert webhook is open-by-default for internal-network deployments.
	// In production we expect the operator to set ALERT_WEBHOOK_TOKEN even on
	// a private network, defence-in-depth.
	if prod && c.Alerting.WebhookToken == "" {
		add("ALERT_WEBHOOK_TOKEN is empty in production — the /webhooks/alerts route accepts unauthenticated pushes")
	}

	// --- Keycloak -------------------------------------------------------------
	// Issuer pointed at localhost in production is almost certainly a misconfig.
	if prod && strings.Contains(c.Keycloak.Issuer, "localhost") {
		add(fmt.Sprintf("KEYCLOAK_ISSUER points at localhost in production: %q", c.Keycloak.Issuer))
	}

	// --- HTTP -----------------------------------------------------------------
	if prod {
		for _, o := range c.HTTP.AllowedOrigins {
			if o == "*" {
				add("HTTP_ALLOWED_ORIGINS contains '*' in production — restrict to known web origins")
				break
			}
			if strings.Contains(o, "localhost") {
				add(fmt.Sprintf("HTTP_ALLOWED_ORIGINS contains a localhost entry in production: %q", o))
			}
		}
	}

	// --- Identity / push (advisory only — empty disables the feature) --------
	if prod {
		if !c.Identity.RequireKYCToBook && !c.Identity.RequireKYCToHost && len(c.Identity.HighValueThresholdsCents) == 0 {
			add("All KYC gates are off in production (REQUIRE_KYC_TO_BOOK, REQUIRE_KYC_TO_HOST, HIGH_VALUE_THRESHOLDS) — confirm this is intentional")
		}
	}

	if prod && len(res.Warnings) > 0 {
		return res, errors.New("config: blocking validation errors in production: " + strings.Join(res.Warnings, "; "))
	}
	return res, nil
}

// requireNonEmpty appends a warning when value is empty or a known
// placeholder. Helper for the payment-gateway block where five fields
// share the same shape.
func requireNonEmpty(res *ValidationResult, name, value string) {
	if value == "" {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s is empty", name))
		return
	}
	if isPlaceholder(value) {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s carries a placeholder value (%q)", name, value))
	}
}

// isPlaceholder catches the well-known weak strings developers paste
// in. NOT an entropy check — strong-but-leaked secrets pass; the
// operator's password rotation policy is the safety net for those.
func isPlaceholder(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "changeme", "change-me", "placeholder", "todo", "secret",
		"password", "admin", "minioadmin", "airhost",
		"test", "example", "your-secret-here", "xxxxxxxx":
		return true
	}
	return false
}
