package config

import (
	"fmt"
	"strings"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all configuration loaded from environment variables.
// Values are populated by envconfig from the process environment.
// See .env.example for all required variables.
type Config struct {
	// Server
	Port string `envconfig:"PORT" default:"8080"`
	Env  string `envconfig:"ENV" default:"development"` // development | production

	// Database
	DatabaseURL string `envconfig:"DATABASE_URL" required:"true"`

	// JWT
	JWTSecret            string `envconfig:"JWT_SECRET" required:"true"`
	JWTAccessExpiryMin   int    `envconfig:"JWT_ACCESS_EXPIRY_MIN" default:"15"`
	JWTRefreshExpiryDays int    `envconfig:"JWT_REFRESH_EXPIRY_DAYS" default:"7"`

	// Password reset
	PasswordResetExpiryMin int `envconfig:"PASSWORD_RESET_EXPIRY_MIN" default:"60"`

	// Frontend base URL (used for password reset / setup links in emails)
	AppBaseURL string `envconfig:"APP_BASE_URL" default:"http://localhost:5173"`

	// CORS
	CORSAllowedOrigins []string `envconfig:"CORS_ALLOWED_ORIGINS" required:"true"`

	// Resend (email)
	ResendAPIKey      string `envconfig:"RESEND_API_KEY" required:"true"`
	ResendFromAddress string `envconfig:"RESEND_FROM_ADDRESS" required:"true"`

	// Twilio (SMS)
	TwilioAccountSID string `envconfig:"TWILIO_ACCOUNT_SID" required:"true"`
	TwilioAuthToken  string `envconfig:"TWILIO_AUTH_TOKEN" required:"true"`
	TwilioFromNumber string `envconfig:"TWILIO_FROM_NUMBER" required:"true"`
	MessagingChannel string `envconfig:"MESSAGING_CHANNEL" default:"sms"` // sms | whatsapp

	// Stripe
	StripeSecretKey     string `envconfig:"STRIPE_SECRET_KEY" required:"true"`
	StripeWebhookSecret string `envconfig:"STRIPE_WEBHOOK_SECRET" required:"true"`

	// GoCardless Direct Debit
	// NOTE: Not active in this release — GoCardless merchant application was rejected
	// before the project deadline. Fields are optional (empty default) so the application
	// starts without credentials. The full integration is preserved in internal/billing/
	// as future development. Set these and remove ErrGoCardlessNotAvailable guards in
	// service.go once a live GoCardless account is available.
	GoCardlessAccessToken   string `envconfig:"GOCARDLESS_ACCESS_TOKEN" default:""`
	GoCardlessWebhookSecret string `envconfig:"GOCARDLESS_WEBHOOK_SECRET" default:""`
	GoCardlessEnv           string `envconfig:"GOCARDLESS_ENV" default:"sandbox"` // sandbox | live

	// Solver microservice (OR-Tools)
	SolverURL            string `envconfig:"SOLVER_URL" default:"http://localhost:8000"`
	SolverTimeoutSeconds int    `envconfig:"SOLVER_TIMEOUT_SECONDS" default:"30"`

	// OpenRouter (AI availability parser — swap model via OPENROUTER_MODEL, no code change needed)
	OpenRouterAPIKey     string `envconfig:"OPENROUTER_API_KEY" default:""`
	OpenRouterModel      string `envconfig:"OPENROUTER_MODEL" default:"openai/gpt-5-nano"`
}

// Load reads configuration from environment variables.
// Returns an error if any required variable is missing.
func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	c.Env = cleanInlineComment(c.Env)
	c.MessagingChannel = cleanInlineComment(c.MessagingChannel)
	c.GoCardlessEnv = cleanInlineComment(c.GoCardlessEnv)
	if len(c.JWTSecret) < 32 {
		return nil, fmt.Errorf("config: JWT_SECRET must be at least 32 bytes (got %d)", len(c.JWTSecret))
	}
	return &c, nil
}

func cleanInlineComment(value string) string {
	beforeComment, _, _ := strings.Cut(value, "#")
	return strings.TrimSpace(beforeComment)
}
