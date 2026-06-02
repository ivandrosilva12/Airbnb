package config

import (
	"strings"
	"testing"
)

// baseConfig builds a Config with the minimum fields a production
// deployment is expected to set. Tests override single fields to
// probe each guard in isolation.
func baseConfig() *Config {
	return &Config{
		App:      AppConfig{Environment: productionEnvName},
		HTTP:     HTTPConfig{AllowedOrigins: []string{"https://airhost.app"}},
		Database: DatabaseConfig{Password: "real-strong-password"},
		Storage:  StorageConfig{AccessKey: "AKIAFRESH", SecretKey: "fresh-secret"},
		Keycloak: KeycloakConfig{Issuer: "https://auth.airhost.app/realms/airhost"},
		Payment: PaymentConfig{
			Provider:   "stripe",
			Stripe:     StripeConfig{SecretKey: "sk_live_x", WebhookSecret: "whsec_x"},
		},
		Alerting: AlertingConfig{WebhookToken: "real-token"},
		Identity: IdentityConfig{RequireKYCToBook: true},
	}
}

// TestValidate_NonProduction_NeverBlocks proves the dev workflow
// stays usable: a config full of placeholders in dev/test environments
// gathers warnings but does NOT return an error.
func TestValidate_NonProduction_NeverBlocks(t *testing.T) {
	c := &Config{
		App:      AppConfig{Environment: "development"},
		HTTP:     HTTPConfig{AllowedOrigins: []string{"*", "http://localhost:5173"}},
		Database: DatabaseConfig{Password: "airhost"},
		Storage:  StorageConfig{AccessKey: "minioadmin", SecretKey: "minioadmin"},
		Keycloak: KeycloakConfig{Issuer: "http://localhost:8080/realms/airhost"},
		Payment:  PaymentConfig{Provider: "fake"},
	}
	res, err := c.Validate()
	if err != nil {
		t.Fatalf("non-production Validate() returned error: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Errorf("expected warnings on a placeholder-heavy config, got none")
	}
}

// TestValidate_Production_BlocksOnPlaceholderDBPassword catches the
// most common deploy disaster — leaving the docker-compose default
// "airhost" password live.
func TestValidate_Production_BlocksOnPlaceholderDBPassword(t *testing.T) {
	c := baseConfig()
	c.Database.Password = "airhost"
	_, err := c.Validate()
	if err == nil {
		t.Fatalf("expected production error for placeholder DB password")
	}
	if !strings.Contains(err.Error(), "DB_PASSWORD") {
		t.Errorf("error doesn't mention DB_PASSWORD: %v", err)
	}
}

// TestValidate_Production_BlocksOnDefaultMinIOCredentials guards
// against the classic minioadmin/minioadmin leak.
func TestValidate_Production_BlocksOnDefaultMinIOCredentials(t *testing.T) {
	c := baseConfig()
	c.Storage.AccessKey = "minioadmin"
	c.Storage.SecretKey = "minioadmin"
	_, err := c.Validate()
	if err == nil {
		t.Fatalf("expected production error for default minio credentials")
	}
}

// TestValidate_Production_StripeProviderRequiresAllSecrets pins the
// per-provider check: when PAYMENT_PROVIDER=stripe, both the secret
// key and the webhook secret are mandatory. Either alone is a partial
// configuration that ships with one of {silent compromise, broken
// payments}.
func TestValidate_Production_StripeProviderRequiresAllSecrets(t *testing.T) {
	c := baseConfig()
	c.Payment.Stripe.WebhookSecret = ""
	_, err := c.Validate()
	if err == nil {
		t.Fatalf("expected production error for missing STRIPE_WEBHOOK_SECRET")
	}
	if !strings.Contains(err.Error(), "STRIPE_WEBHOOK_SECRET") {
		t.Errorf("error doesn't mention STRIPE_WEBHOOK_SECRET: %v", err)
	}
}

// TestValidate_Production_AutoProviderRequires_AllGatewaysConfigured
// is the broadest path: provider=auto routes by currency, so every
// gateway must be fully configured. A missing key on the seldom-used
// path would surface only when the unlucky currency shows up.
func TestValidate_Production_AutoProviderRequires_AllGatewaysConfigured(t *testing.T) {
	c := baseConfig()
	c.Payment.Provider = "auto"
	c.Payment.GPayAngola = GPayAngolaConfig{APIKey: "gp_x", WebhookSecret: "gp_wh"}
	c.Payment.AppyPay = AppyPayConfig{WebhookSecret: "ap_wh"}
	// Stripe already set in baseConfig. Try clearing GPay → should err.
	c.Payment.GPayAngola.APIKey = ""
	_, err := c.Validate()
	if err == nil {
		t.Fatalf("expected production error for incomplete auto-routing config")
	}
}

// TestValidate_Production_WildcardCORSBlocked closes the most common
// CORS-mistake: shipping "*" with credentials enabled. Empty origins
// is fine (CORS off), but wildcards are not.
func TestValidate_Production_WildcardCORSBlocked(t *testing.T) {
	c := baseConfig()
	c.HTTP.AllowedOrigins = []string{"*"}
	_, err := c.Validate()
	if err == nil {
		t.Fatalf("expected production error for wildcard CORS")
	}
}

// TestValidate_Production_LocalhostKeycloakBlocked stops a deploy with
// the dev Keycloak issuer baked in.
func TestValidate_Production_LocalhostKeycloakBlocked(t *testing.T) {
	c := baseConfig()
	c.Keycloak.Issuer = "http://localhost:8080/realms/airhost"
	_, err := c.Validate()
	if err == nil {
		t.Fatalf("expected production error for localhost Keycloak issuer")
	}
}

// TestValidate_Production_HappyPath confirms a real-looking
// production config passes without complaint. If a future tightening
// makes baseConfig() invalid, this test forces an update to the
// baseline (and to the docs).
func TestValidate_Production_HappyPath(t *testing.T) {
	c := baseConfig()
	res, err := c.Validate()
	if err != nil {
		t.Fatalf("baseConfig should pass: %v (warnings: %v)", err, res.Warnings)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("baseConfig should have zero warnings, got: %v", res.Warnings)
	}
}

// TestIsPlaceholder_KnownWeakStrings pins the catalogue of strings
// the heuristic catches — a regression here means a deploy might
// pass with "changeme" as a webhook secret.
func TestIsPlaceholder_KnownWeakStrings(t *testing.T) {
	for _, weak := range []string{"changeme", "CHANGEME", "ChangeMe ", "password", "admin", "minioadmin", "test", ""} {
		if !isPlaceholder(weak) {
			t.Errorf("isPlaceholder(%q) = false, want true", weak)
		}
	}
	for _, strong := range []string{"sk_live_xxx", "AKIA0123456789", "real-strong-password"} {
		if isPlaceholder(strong) {
			t.Errorf("isPlaceholder(%q) = true, want false", strong)
		}
	}
}
