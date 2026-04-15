package billing

import (
	"context"
	"fmt"

	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/customer"
	"github.com/stripe/stripe-go/v78/paymentintent"
	"github.com/stripe/stripe-go/v78/setupintent"
	"github.com/stripe/stripe-go/v78/webhook"
)

// StripeClient wraps the Stripe SDK and exposes only the operations we need.
type StripeClient struct {
	secretKey      string
	webhookSecret  string
}

func NewStripeClient(secretKey, webhookSecret string) *StripeClient {
	stripe.Key = secretKey
	return &StripeClient{secretKey: secretKey, webhookSecret: webhookSecret}
}

// CreateOrGetCustomer returns an existing Stripe customer ID or creates a new one.
func (s *StripeClient) CreateOrGetCustomer(ctx context.Context, email, name string) (string, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
		Name:  stripe.String(name),
	}
	params.AddMetadata("source", "pt-scheduler")

	c, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("stripe: create customer: %w", err)
	}
	return c.ID, nil
}

// CreateSetupIntent creates a Stripe SetupIntent so the client can save a card
// without an immediate charge. Returns the client_secret for the frontend.
// SCA/3DS2 is automatically handled by the Stripe Elements SDK on the frontend.
func (s *StripeClient) CreateSetupIntent(ctx context.Context, customerID string) (*SetupIntentResponse, error) {
	params := &stripe.SetupIntentParams{
		Customer:           stripe.String(customerID),
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
	}
	params.AddMetadata("source", "pt-scheduler")

	si, err := setupintent.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create setup intent: %w", err)
	}

	return &SetupIntentResponse{
		ClientSecret:  si.ClientSecret,
		SetupIntentID: si.ID,
	}, nil
}

// ChargeMonthly creates a PaymentIntent for the monthly subscription amount.
// Uses `off_session: true` as the client is not present when billing runs.
// If SCA is required, the PaymentIntent will have status requires_action —
// the webhook handles this by alerting the coach to prompt the client.
func (s *StripeClient) ChargeMonthly(ctx context.Context, customerID, idempotencyKey string, amountPence int) (string, error) {
	params := &stripe.PaymentIntentParams{
		Amount:        stripe.Int64(int64(amountPence)),
		Currency:      stripe.String("gbp"),
		Customer:      stripe.String(customerID),
		Confirm:       stripe.Bool(true),
		OffSession:    stripe.Bool(true),
		// Automatically use the customer's default saved payment method
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
	}
	params.AddMetadata("source", "pt-scheduler")
	params.SetIdempotencyKey(idempotencyKey)

	pi, err := paymentintent.New(params)
	if err != nil {
		return "", fmt.Errorf("stripe: create payment intent: %w", err)
	}
	return pi.ID, nil
}

// VerifyWebhookSignature validates the Stripe-Signature header and returns the parsed event.
// rawBody must be the exact bytes received from Stripe — never re-encode.
func (s *StripeClient) VerifyWebhookSignature(rawBody []byte, sigHeader string) (*stripe.Event, error) {
	event, err := webhook.ConstructEvent(rawBody, sigHeader, s.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("stripe: webhook signature invalid: %w", err)
	}
	return &event, nil
}
