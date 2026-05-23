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
}

// PricingConfig holds platform pricing policy.
type PricingConfig struct {
	ServiceFeeRate float64 // platform fee fraction, e.g. 0.12 for 12%
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
