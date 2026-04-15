package billing

import (
	"time"

	"github.com/google/uuid"
)

// Provider constants
const (
	ProviderStripe      = "stripe"
	ProviderGoCardless  = "gocardless"
)

// Payment statuses (mirror the DB CHECK constraint)
const (
	PaymentStatusPending  = "pending"
	PaymentStatusPaid     = "paid"
	PaymentStatusFailed   = "failed"
	PaymentStatusRefunded = "refunded"
)

// Payment represents one monthly charge for a client.
type Payment struct {
	ID             uuid.UUID `json:"id"`
	ClientID       uuid.UUID `json:"client_id"`
	Provider       string    `json:"provider"`
	ProviderRef    *string   `json:"provider_ref,omitempty"`
	AmountPence    int       `json:"amount_pence"`
	Currency       string    `json:"currency"`
	BillingYear    int       `json:"billing_year"`
	BillingMonth   int       `json:"billing_month"`
	Status         string    `json:"status"`
	IdempotencyKey string    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Mandate represents a GoCardless Direct Debit mandate for a client.
type Mandate struct {
	ID         uuid.UUID `json:"id"`
	ClientID   uuid.UUID `json:"client_id"`
	MandateID  string    `json:"mandate_id"`  // GoCardless mandate ID
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// WebhookEvent is stored to prevent duplicate webhook processing.
type WebhookEvent struct {
	ID         uuid.UUID `json:"id"`
	Provider   string    `json:"provider"`
	EventID    string    `json:"event_id"`
	ReceivedAt time.Time `json:"received_at"`
}

// --- Request/response types ---

// SetupIntentRequest is the body for POST /api/v1/payments/setup-intent
// The frontend uses the returned client_secret to render Stripe Elements and
// collect card details without sensitive data touching our server.
type SetupIntentRequest struct {
	ClientID string `json:"client_id" validate:"required,uuid4"`
}

// SetupIntentResponse is returned after creating a Stripe SetupIntent.
type SetupIntentResponse struct {
	ClientSecret string `json:"client_secret"`
	SetupIntentID string `json:"setup_intent_id"`
}

// MandateRequest is the body for POST /api/v1/payments/mandate
// The frontend redirects the client to the GoCardless billing page.
type MandateRequest struct {
	ClientID    string `json:"client_id"    validate:"required,uuid4"`
	RedirectURI string `json:"redirect_uri" validate:"required,url"`
}

// MandateResponse is returned after creating a GoCardless redirect flow.
type MandateResponse struct {
	RedirectURL string `json:"redirect_url"` // FRONTEND: redirect client to this URL
	FlowID      string `json:"flow_id"`
}

// ChargeRequest is the body for POST /api/v1/billing/charge
// Allows a coach to manually trigger the monthly charge for a client.
type ChargeRequest struct {
	ClientID     string `json:"client_id"     validate:"required,uuid4"`
	AmountPence  int    `json:"amount_pence"  validate:"required,min=100"`
	BillingYear  int    `json:"billing_year"  validate:"required,min=2024"`
	BillingMonth int    `json:"billing_month" validate:"required,min=1,max=12"`
}

// IdempotencyKey builds the unique key for a monthly payment.
// Format: {provider}-{client_id}-{year}-{month:02d}
func IdempotencyKey(provider string, clientID uuid.UUID, year, month int) string {
	return provider + "-" + clientID.String() + "-" + pad4(year) + "-" + pad2(month)
}

func pad4(n int) string {
	s := "0000" + itoa(n)
	return s[len(s)-4:]
}

func pad2(n int) string {
	s := "00" + itoa(n)
	return s[len(s)-2:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
