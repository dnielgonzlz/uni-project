package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all configuration loaded from environment variables.
// Values are populated by envconfig from the process environment.
// See .env.example for all required variables.
type Config struct {
	// Server
	Port     string `envconfig:"PORT" default:"8080"`
	Env      string `envconfig:"ENV" default:"development"` // development | production

	// Database
	DatabaseURL string `envconfig:"DATABASE_URL" required:"true"`

	// JWT
	JWTSecret            string `envconfig:"JWT_SECRET" required:"true"`
	JWTAccessExpiryMin   int    `envconfig:"JWT_ACCESS_EXPIRY_MIN" default:"15"`
	JWTRefreshExpiryDays int    `envconfig:"JWT_REFRESH_EXPIRY_DAYS" default:"7"`

	// Password reset
	PasswordResetExpiryMin int `envconfig:"PASSWORD_RESET_EXPIRY_MIN" default:"60"`

	// CORS
	CORSAllowedOrigins []string `envconfig:"CORS_ALLOWED_ORIGINS" required:"true"`

	// Resend (email)
	ResendAPIKey       string `envconfig:"RESEND_API_KEY" required:"true"`
	ResendFromAddress  string `envconfig:"RESEND_FROM_ADDRESS" required:"true"`

	// Twilio (SMS)
	TwilioAccountSID  string `envconfig:"TWILIO_ACCOUNT_SID" required:"true"`
	TwilioAuthToken   string `envconfig:"TWILIO_AUTH_TOKEN" required:"true"`
	TwilioFromNumber  string `envconfig:"TWILIO_FROM_NUMBER" required:"true"`
	MessagingChannel  string `envconfig:"MESSAGING_CHANNEL" default:"sms"` // sms | whatsapp

	// Stripe
	StripeSecretKey      string `envconfig:"STRIPE_SECRET_KEY" required:"true"`
	StripeWebhookSecret  string `envconfig:"STRIPE_WEBHOOK_SECRET" required:"true"`

	// GoCardless
	GoCardlessAccessToken  string `envconfig:"GOCARDLESS_ACCESS_TOKEN" required:"true"`
	GoCardlessWebhookSecret string `envconfig:"GOCARDLESS_WEBHOOK_SECRET" required:"true"`
	GoCardlessEnv          string `envconfig:"GOCARDLESS_ENV" default:"sandbox"` // sandbox | live

	// Solver microservice (OR-Tools)
	SolverURL            string `envconfig:"SOLVER_URL" default:"http://localhost:8000"`
	SolverTimeoutSeconds int    `envconfig:"SOLVER_TIMEOUT_SECONDS" default:"30"`

	// Sentry
	SentryDSN string `envconfig:"SENTRY_DSN"` // optional; empty disables Sentry
}

// Load reads configuration from environment variables.
// Returns an error if any required variable is missing.
func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}
