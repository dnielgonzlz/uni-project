package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/customer"
	"github.com/stripe/stripe-go/v78/paymentintent"
	"github.com/stripe/stripe-go/v78/paymentmethod"
	"github.com/stripe/stripe-go/v78/price"
	"github.com/stripe/stripe-go/v78/product"
	"github.com/stripe/stripe-go/v78/setupintent"
	stripesubscription "github.com/stripe/stripe-go/v78/subscription"
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

// StripeSubscriptionResult holds the data returned after creating a Stripe subscription.
type StripeSubscriptionResult struct {
	SubscriptionID string
	Status         string
	PeriodStart    time.Time
	PeriodEnd      time.Time
}

// CreateProduct creates a Stripe Product for a subscription plan.
func (s *StripeClient) CreateProduct(ctx context.Context, name, description string) (string, error) {
	params := &stripe.ProductParams{
		Name: stripe.String(name),
	}
	if description != "" {
		params.Description = stripe.String(description)
	}
	params.AddMetadata("source", "pt-scheduler")

	p, err := product.New(params)
	if err != nil {
		return "", fmt.Errorf("stripe: create product: %w", err)
	}
	return p.ID, nil
}

// CreatePrice creates a recurring monthly GBP price for a product.
func (s *StripeClient) CreatePrice(ctx context.Context, productID string, amountPence int) (string, error) {
	params := &stripe.PriceParams{
		Product:    stripe.String(productID),
		UnitAmount: stripe.Int64(int64(amountPence)),
		Currency:   stripe.String("gbp"),
		Recurring: &stripe.PriceRecurringParams{
			Interval: stripe.String("month"),
		},
	}

	p, err := price.New(params)
	if err != nil {
		return "", fmt.Errorf("stripe: create price: %w", err)
	}
	return p.ID, nil
}

// ArchiveProduct deactivates a Stripe Product so it can no longer be used.
func (s *StripeClient) ArchiveProduct(ctx context.Context, productID string) error {
	params := &stripe.ProductParams{Active: stripe.Bool(false)}
	if _, err := product.Update(productID, params); err != nil {
		return fmt.Errorf("stripe: archive product: %w", err)
	}
	return nil
}

// ArchivePrice deactivates a Stripe Price.
func (s *StripeClient) ArchivePrice(ctx context.Context, priceID string) error {
	params := &stripe.PriceParams{Active: stripe.Bool(false)}
	if _, err := price.Update(priceID, params); err != nil {
		return fmt.Errorf("stripe: archive price: %w", err)
	}
	return nil
}

// GetDefaultPaymentMethod returns the first saved card payment method for a customer.
func (s *StripeClient) GetDefaultPaymentMethod(ctx context.Context, customerID string) (string, error) {
	params := &stripe.PaymentMethodListParams{
		Customer: stripe.String(customerID),
		Type:     stripe.String("card"),
	}
	iter := paymentmethod.List(params)
	for iter.Next() {
		return iter.PaymentMethod().ID, nil
	}
	if err := iter.Err(); err != nil {
		return "", fmt.Errorf("stripe: list payment methods: %w", err)
	}
	return "", fmt.Errorf("stripe: no payment method on file for customer %s", customerID)
}

// CreateSubscription creates a Stripe Subscription for a customer on a price.
func (s *StripeClient) CreateSubscription(ctx context.Context, customerID, priceID, paymentMethodID string) (*StripeSubscriptionResult, error) {
	params := &stripe.SubscriptionParams{
		Customer: stripe.String(customerID),
		Items: []*stripe.SubscriptionItemsParams{
			{Price: stripe.String(priceID)},
		},
		DefaultPaymentMethod: stripe.String(paymentMethodID),
	}
	params.AddMetadata("source", "pt-scheduler")

	s2, err := stripesubscription.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create subscription: %w", err)
	}

	return &StripeSubscriptionResult{
		SubscriptionID: s2.ID,
		Status:         string(s2.Status),
		PeriodStart:    time.Unix(s2.CurrentPeriodStart, 0).UTC(),
		PeriodEnd:      time.Unix(s2.CurrentPeriodEnd, 0).UTC(),
	}, nil
}

// CancelSubscription cancels a Stripe Subscription immediately.
func (s *StripeClient) CancelSubscription(ctx context.Context, stripeSubID string) error {
	if _, err := stripesubscription.Cancel(stripeSubID, nil); err != nil {
		return fmt.Errorf("stripe: cancel subscription: %w", err)
	}
	return nil
}

// UpdateSubscriptionPrice swaps the price on an existing subscription's first item.
func (s *StripeClient) UpdateSubscriptionPrice(ctx context.Context, stripeSubID, newPriceID string) error {
	// Fetch the subscription to get the item ID
	existing, err := stripesubscription.Get(stripeSubID, nil)
	if err != nil {
		return fmt.Errorf("stripe: get subscription for update: %w", err)
	}
	if len(existing.Items.Data) == 0 {
		return fmt.Errorf("stripe: subscription has no items")
	}
	itemID := existing.Items.Data[0].ID

	params := &stripe.SubscriptionParams{
		Items: []*stripe.SubscriptionItemsParams{
			{ID: stripe.String(itemID), Price: stripe.String(newPriceID)},
		},
	}
	if _, err := stripesubscription.Update(stripeSubID, params); err != nil {
		return fmt.Errorf("stripe: update subscription price: %w", err)
	}
	return nil
}
