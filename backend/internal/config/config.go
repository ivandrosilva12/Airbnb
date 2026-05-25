// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config aggregates all runtime configuration for the API.
type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Database DatabaseConfig
	Keycloak KeycloakConfig
	Storage  StorageConfig
	Pricing  PricingConfig
	Email    EmailConfig
	Payment  PaymentConfig
	Security SecurityConfig
	Alerting AlertingConfig
}

// AlertingConfig holds settings for operating the alerting stack. When
// AlertmanagerURL is empty, silence management is disabled (the endpoints report
// the feature as unavailable rather than calling out).
type AlertingConfig struct {
	AlertmanagerURL string // e.g. http://alertmanager:9093
	// WebhookToken, when set, is required as a bearer on POST /webhooks/alerts so
	// only Alertmanager can push alert state. Empty disables the check (the
	// internal-network default).
	WebhookToken string
}

// PricingConfig holds platform pricing policy.
type PricingConfig struct {
	ServiceFeeRate float64 // platform fee fraction, e.g. 0.12 for 12%
}

// SecurityConfig holds request-protection tunables.
type SecurityConfig struct {
	// WebhookRateRPS / WebhookRateBurst configure the per-IP token bucket
	// guarding the unauthenticated payment-webhook route.
	WebhookRateRPS   float64
	WebhookRateBurst int
	// WebhookRetentionDays is how long processed-webhook dedupe records are kept;
	// WebhookCleanupInterval is how often the scheduled cleanup runs.
	WebhookRetentionDays   int
	WebhookCleanupInterval time.Duration
	// MaxRequestBodyBytes caps non-multipart request bodies (file uploads manage
	// their own, larger limit). Guards against memory-amplification DoS.
	MaxRequestBodyBytes int64
	// APIRateRPS / APIRateBurst configure a per-IP token bucket over the whole
	// /api/v1 surface. When APIRateRPS is <= 0 the limiter is disabled (the
	// default in tests); production sets a sane positive default.
	APIRateRPS   float64
	APIRateBurst int
}

// PaymentConfig selects and configures the payment gateway. Provider is one of
// "fake" (default; no external calls), "stripe", "appypay", "gpayangola", or
// "auto" (route by currency: domestic → GPay Angola with AppyPay failover,
// foreign → Stripe). Secrets come from the environment and must never be
// committed.
type PaymentConfig struct {
	Provider string
	// DomesticCurrency is the ISO currency treated as a domestic (Angola)
	// payment in "auto" routing; anything else routes to the foreign gateway.
	DomesticCurrency string
	Stripe           StripeConfig
	AppyPay          AppyPayConfig
	GPayAngola       GPayAngolaConfig
}

// StripeConfig holds Stripe REST API settings.
type StripeConfig struct {
	SecretKey     string // sk_test_… / sk_live_…
	BaseURL       string // override for tests; defaults to https://api.stripe.com
	WebhookSecret string // whsec_…; verifies inbound webhook signatures
	// ConnectCountry is the country code used when creating hosts' Stripe Connect
	// Express accounts (must be a Connect-supported country); defaults to US.
	ConnectCountry string
}

// AppyPayConfig holds AppyPay REST API settings. Token is a pre-minted OAuth2
// bearer (client-credentials) access token; in production it would be refreshed
// out of band and injected via the environment.
type AppyPayConfig struct {
	Token         string
	BaseURL       string // e.g. https://api.appypay.co.ao/v2.0
	WebhookSecret string // shared secret verifying inbound webhook signatures
}

// GPayAngolaConfig holds GPay Angola (gpayangola.com) REST API settings. APIKey
// authenticates as a bearer token.
type GPayAngolaConfig struct {
	APIKey        string
	BaseURL       string // e.g. https://api.gpayangola.com
	WebhookSecret string // shared secret verifying inbound webhook signatures
}

// EmailConfig holds transactional email settings. When SMTPHost is empty, a
// logging mailer is used instead of sending real email.
type EmailConfig struct {
	FromAddress  string
	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPassword string
}

// AppConfig holds general application settings.
type AppConfig struct {
	Name        string
	Environment string
}

// HTTPConfig holds the HTTP server settings.
type HTTPConfig struct {
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	AllowedOrigins  []string
	// TrustedProxies is the list of proxy IPs/CIDRs whose X-Forwarded-For is
	// honoured for the client IP (used by the rate limiter). Empty means trust
	// none — the direct peer address is used, so clients cannot spoof their IP.
	TrustedProxies []string
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	Host           string
	Port           string
	User           string
	Password       string
	Name           string
	SSLMode        string
	MaxConns       int32
	MigrationsPath string
}

// DSN returns a pgx-compatible connection string.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode,
	)
}

// KeycloakConfig holds OIDC settings for token verification.
type KeycloakConfig struct {
	Issuer   string // e.g. http://localhost:8080/realms/airhost
	ClientID string // expected audience
}

// StorageConfig holds MinIO/S3 settings.
type StorageConfig struct {
	Endpoint   string
	AccessKey  string
	SecretKey  string
	Bucket     string
	UseSSL     bool
	PublicHost string // host used to build public object URLs
}

// Load reads configuration from the environment, optionally loading a .env file.
func Load() (*Config, error) {
	// Best-effort: a missing .env file is not an error.
	_ = godotenv.Load()

	cfg := &Config{
		App: AppConfig{
			Name:        getEnv("APP_NAME", "airhost-api"),
			Environment: getEnv("APP_ENV", "development"),
		},
		HTTP: HTTPConfig{
			Port:            getEnv("HTTP_PORT", "8081"),
			ReadTimeout:     getDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getDuration("HTTP_WRITE_TIMEOUT", 15*time.Second),
			ShutdownTimeout: getDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
			AllowedOrigins:  getSlice("HTTP_ALLOWED_ORIGINS", []string{"http://localhost:5173", "http://localhost:3000"}),
			TrustedProxies:  getSlice("HTTP_TRUSTED_PROXIES", nil),
		},
		Database: DatabaseConfig{
			Host:           getEnv("DB_HOST", "localhost"),
			Port:           getEnv("DB_PORT", "5432"),
			User:           getEnv("DB_USER", "airhost"),
			Password:       getEnv("DB_PASSWORD", "airhost"),
			Name:           getEnv("DB_NAME", "airhost"),
			SSLMode:        getEnv("DB_SSLMODE", "disable"),
			MaxConns:       int32(getInt("DB_MAX_CONNS", 10)),
			MigrationsPath: getEnv("DB_MIGRATIONS_PATH", "internal/infrastructure/persistence/postgres/migrations"),
		},
		Keycloak: KeycloakConfig{
			Issuer:   getEnv("KEYCLOAK_ISSUER", "http://localhost:8080/realms/airhost"),
			ClientID: getEnv("KEYCLOAK_CLIENT_ID", "airhost-api"),
		},
		Storage: StorageConfig{
			Endpoint:   getEnv("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey:  getEnv("MINIO_ACCESS_KEY", "minioadmin"),
			SecretKey:  getEnv("MINIO_SECRET_KEY", "minioadmin"),
			Bucket:     getEnv("MINIO_BUCKET", "airhost-media"),
			UseSSL:     getBool("MINIO_USE_SSL", false),
			PublicHost: getEnv("MINIO_PUBLIC_HOST", "http://localhost:9000"),
		},
		Pricing: PricingConfig{
			ServiceFeeRate: getFloat("SERVICE_FEE_RATE", 0.12),
		},
		Email: EmailConfig{
			FromAddress:  getEnv("EMAIL_FROM", "no-reply@airhost.dev"),
			SMTPHost:     getEnv("SMTP_HOST", ""),
			SMTPPort:     getEnv("SMTP_PORT", "1025"),
			SMTPUser:     getEnv("SMTP_USER", ""),
			SMTPPassword: getEnv("SMTP_PASSWORD", ""),
		},
		Payment: PaymentConfig{
			Provider:         getEnv("PAYMENT_PROVIDER", "fake"),
			DomesticCurrency: getEnv("PAYMENT_DOMESTIC_CURRENCY", "AOA"),
			Stripe: StripeConfig{
				SecretKey:      getEnv("STRIPE_SECRET_KEY", ""),
				BaseURL:        getEnv("STRIPE_BASE_URL", "https://api.stripe.com"),
				WebhookSecret:  getEnv("STRIPE_WEBHOOK_SECRET", ""),
				ConnectCountry: getEnv("STRIPE_CONNECT_COUNTRY", "US"),
			},
			AppyPay: AppyPayConfig{
				Token:         getEnv("APPYPAY_TOKEN", ""),
				BaseURL:       getEnv("APPYPAY_BASE_URL", "https://api.appypay.co.ao/v2.0"),
				WebhookSecret: getEnv("APPYPAY_WEBHOOK_SECRET", ""),
			},
			GPayAngola: GPayAngolaConfig{
				APIKey:        getEnv("GPAYANGOLA_API_KEY", ""),
				BaseURL:       getEnv("GPAYANGOLA_BASE_URL", "https://api.gpayangola.com"),
				WebhookSecret: getEnv("GPAYANGOLA_WEBHOOK_SECRET", ""),
			},
		},
		Security: SecurityConfig{
			WebhookRateRPS:         getFloat("WEBHOOK_RATE_RPS", 5),
			WebhookRateBurst:       getInt("WEBHOOK_RATE_BURST", 20),
			WebhookRetentionDays:   getInt("WEBHOOK_RETENTION_DAYS", 30),
			WebhookCleanupInterval: getDuration("WEBHOOK_CLEANUP_INTERVAL", 24*time.Hour),
			MaxRequestBodyBytes:    int64(getInt("MAX_REQUEST_BODY_BYTES", 1<<20)),
			APIRateRPS:             getFloat("API_RATE_RPS", 30),
			APIRateBurst:           getInt("API_RATE_BURST", 60),
		},
		Alerting: AlertingConfig{
			AlertmanagerURL: getEnv("ALERTMANAGER_URL", ""),
			WebhookToken:    getEnv("ALERT_WEBHOOK_TOKEN", ""),
		},
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getFloat(key string, fallback float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getSlice(key string, fallback []string) []string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		var out []string
		start := 0
		for i := 0; i <= len(v); i++ {
			if i == len(v) || v[i] == ',' {
				item := v[start:i]
				// trim spaces
				for len(item) > 0 && item[0] == ' ' {
					item = item[1:]
				}
				for len(item) > 0 && item[len(item)-1] == ' ' {
					item = item[:len(item)-1]
				}
				if item != "" {
					out = append(out, item)
				}
				start = i + 1
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return fallback
}
