package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// ErrGoCardlessNotAvailable is returned by all GoCardless methods.
//
// The GoCardless merchant application was rejected before the project submission deadline
// and there was insufficient time to re-apply or switch to an alternative Direct Debit
// provider. The full integration has been written and is preserved in gocardless.go,
// service.go, handler.go, and repository.go as an obvious future development path for
// this UK-localised product. Re-enabling it requires only a live GoCardless account and
// the removal of these early-return guards.
var ErrGoCardlessNotAvailable = errors.New("GoCardless Direct Debit integration is not available in this release")

// userLookup is the narrow slice of the users.Repository needed by this package.
type userLookup interface {
	GetClientByID(ctx context.Context, id uuid.UUID) (*users.Client, error)
	GetCoachByID(ctx context.Context, id uuid.UUID) (*users.Coach, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*users.User, error)
}

// PaymentNotifier is satisfied by messaging.NotificationService.
type PaymentNotifier interface {
	EnqueuePaymentFailedAlert(ctx context.Context, coachID, clientID uuid.UUID,
		coachName, coachEmail, coachPhone, clientName string,
		year, month int, provider string) error
}

// Service orchestrates all billing operations.
type Service struct {
	repo     *Repository
	stripe   *StripeClient
	gc       *GoCardlessClient
	logger   *slog.Logger
	users    userLookup
	notifier PaymentNotifier
	subRepo  *SubscriptionRepository
	subSvc   *SubscriptionService
}

func NewService(repo *Repository, stripe *StripeClient, gc *GoCardlessClient, logger *slog.Logger) *Service {
	return &Service{repo: repo, stripe: stripe, gc: gc, logger: logger}
}

// WithUserLookup injects the user repository so the service can look up names/contacts
// for notification payloads. Call this once during wiring in main.go.
func (s *Service) WithUserLookup(u userLookup) *Service {
	s.users = u
	return s
}

// WithNotifier injects the notification service. Call this once during wiring in main.go.
func (s *Service) WithNotifier(n PaymentNotifier) *Service {
	s.notifier = n
	return s
}

// WithSubscriptionService injects the subscription service for webhook handling.
func (s *Service) WithSubscriptionService(subRepo *SubscriptionRepository, subSvc *SubscriptionService) *Service {
	s.subRepo = subRepo
	s.subSvc = subSvc
	return s
}

// --- Stripe card setup ---

// CreateSetupIntent creates a Stripe SetupIntent and returns the client_secret.
// The frontend uses this to render Stripe Elements and collect the card.
// FRONTEND: display Stripe Elements with the returned client_secret; on success call
// your backend to confirm the payment method is attached to the customer.
func (s *Service) CreateSetupIntent(ctx context.Context, clientID uuid.UUID, email, fullName string) (*SetupIntentResponse, error) {
	customerID, err := s.stripe.CreateOrGetCustomer(ctx, email, fullName)
	if err != nil {
		return nil, fmt.Errorf("billing: create setup intent: %w", err)
	}

	// Store the Stripe customer ID so we can create subscriptions later.
	if s.subRepo != nil {
		if err := s.subRepo.SetStripeCustomerID(ctx, clientID, customerID); err != nil {
			s.logger.WarnContext(ctx, "billing: failed to store stripe customer id", "client_id", clientID, "error", err)
		}
	}

	resp, err := s.stripe.CreateSetupIntent(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("billing: create setup intent: %w", err)
	}
	return resp, nil
}

// --- GoCardless Direct Debit setup ---

// CreateMandateFlow starts a GoCardless redirect flow and returns the URL to redirect the client to.
// FRONTEND: redirect the client to the returned redirect_url; after they authorise,
// GoCardless redirects them back to redirect_uri — call POST /api/v1/payments/mandate/complete
// with the redirect_flow_id query param to finalise the mandate.
//
// NOT ACTIVE: GoCardless application was rejected. See ErrGoCardlessNotAvailable.
func (s *Service) CreateMandateFlow(ctx context.Context, clientID uuid.UUID, redirectURI string) (*MandateResponse, error) {
	return nil, ErrGoCardlessNotAvailable
	// Preserved implementation — uncomment when GoCardless account is approved:
	// sessionToken := clientID.String()
	// redirectURL, flowID, err := s.gc.CreateRedirectFlow(
	// 	ctx,
	// 	"PT Scheduler monthly training subscription",
	// 	redirectURI,
	// 	sessionToken,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("billing: create mandate flow: %w", err)
	// }
	// return &MandateResponse{RedirectURL: redirectURL, FlowID: flowID}, nil
}

// CompleteMandateFlow finalises a GoCardless redirect flow and stores the mandate.
//
// NOT ACTIVE: GoCardless application was rejected. See ErrGoCardlessNotAvailable.
func (s *Service) CompleteMandateFlow(ctx context.Context, clientID uuid.UUID, flowID string) (*Mandate, error) {
	return nil, ErrGoCardlessNotAvailable
	// Preserved implementation — uncomment when GoCardless account is approved:
	// mandateID, err := s.gc.CompleteRedirectFlow(ctx, flowID, clientID.String())
	// if err != nil {
	// 	return nil, fmt.Errorf("billing: complete mandate: %w", err)
	// }
	// mandate, err := s.repo.UpsertMandate(ctx, clientID, mandateID, "active")
	// if err != nil {
	// 	return nil, fmt.Errorf("billing: store mandate: %w", err)
	// }
	// return mandate, nil
}

// --- Monthly billing ---

// ChargeMonthly triggers a monthly payment for a client via their configured provider.
// Idempotent: calling twice for the same client/month is safe.
func (s *Service) ChargeMonthly(ctx context.Context, clientID uuid.UUID, provider string, amountPence, year, month int) (*Payment, error) {
	ikey := IdempotencyKey(provider, clientID, year, month)

	payment := &Payment{
		ClientID:       clientID,
		Provider:       provider,
		AmountPence:    amountPence,
		Currency:       "GBP",
		BillingYear:    year,
		BillingMonth:   month,
		Status:         PaymentStatusPending,
		IdempotencyKey: ikey,
	}

	created, err := s.repo.CreatePayment(ctx, payment)
	if errors.Is(err, ErrAlreadyPaid) {
		s.logger.InfoContext(ctx, "payment already exists, skipping", "client_id", clientID, "year", year, "month", month)
		return nil, ErrAlreadyPaid
	}
	if err != nil {
		return nil, fmt.Errorf("billing: create payment record: %w", err)
	}

	switch provider {
	case ProviderStripe:
		err = s.chargeStripe(ctx, created)
	case ProviderGoCardless:
		err = s.chargeGoCardless(ctx, created)
	default:
		return nil, fmt.Errorf("billing: unknown provider: %s", provider)
	}

	if err != nil {
		// Mark payment as failed but don't delete it — preserves the idempotency key.
		_ = s.repo.UpdatePaymentStatus(ctx, created.ID, PaymentStatusFailed, nil)
		return nil, fmt.Errorf("billing: charge failed: %w", err)
	}

	return created, nil
}

func (s *Service) chargeStripe(ctx context.Context, p *Payment) error {
	// The customer ID would normally be stored on the client profile.
	// For MVP we re-create or look up the customer each time using the client's user email.
	// TODO Phase 5: store stripe_customer_id on coaches.stripe_account_id equivalent for clients.
	piID, err := s.stripe.ChargeMonthly(ctx, "" /* customerID */, p.IdempotencyKey, p.AmountPence)
	if err != nil {
		return err
	}
	return s.repo.UpdatePaymentStatus(ctx, p.ID, PaymentStatusPending, &piID)
}

// chargeGoCardless submits a Bacs Direct Debit payment against the client's mandate.
//
// NOT ACTIVE: GoCardless application was rejected. See ErrGoCardlessNotAvailable.
func (s *Service) chargeGoCardless(ctx context.Context, p *Payment) error {
	return ErrGoCardlessNotAvailable
	// Preserved implementation — uncomment when GoCardless account is approved:
	// mandate, err := s.repo.GetMandateByClientID(ctx, p.ClientID)
	// if err != nil {
	// 	return fmt.Errorf("billing: get mandate: %w", err)
	// }
	// chargeDate := BacsEarliestChargeDate(time.Now().UTC(), false)
	// _ = chargeDate
	// gcPaymentID, err := s.gc.CreatePayment(
	// 	ctx,
	// 	mandate.MandateID,
	// 	p.IdempotencyKey,
	// 	p.AmountPence,
	// 	fmt.Sprintf("PT training — %d/%02d", p.BillingYear, p.BillingMonth),
	// )
	// if err != nil {
	// 	return err
	// }
	// return s.repo.UpdatePaymentStatus(ctx, p.ID, PaymentStatusPending, &gcPaymentID)
}

// --- Webhook handlers ---

// notifyPaymentFailed looks up the client and coach from the payment record and
// enqueues a failure alert for the coach. Errors are logged but never returned —
// a missing notification must not fail the webhook acknowledgement.
func (s *Service) notifyPaymentFailed(ctx context.Context, p *Payment) {
	if s.notifier == nil || s.users == nil {
		return
	}

	client, err := s.users.GetClientByID(ctx, p.ClientID)
	if err != nil {
		s.logger.WarnContext(ctx, "billing: notify payment failed — client lookup", "client_id", p.ClientID, "error", err)
		return
	}
	clientUser, err := s.users.GetUserByID(ctx, client.UserID)
	if err != nil {
		s.logger.WarnContext(ctx, "billing: notify payment failed — client user lookup", "user_id", client.UserID, "error", err)
		return
	}
	coach, err := s.users.GetCoachByID(ctx, client.CoachID)
	if err != nil {
		s.logger.WarnContext(ctx, "billing: notify payment failed — coach lookup", "coach_id", client.CoachID, "error", err)
		return
	}
	coachUser, err := s.users.GetUserByID(ctx, coach.UserID)
	if err != nil {
		s.logger.WarnContext(ctx, "billing: notify payment failed — coach user lookup", "user_id", coach.UserID, "error", err)
		return
	}

	coachPhone := ""
	if coachUser.PhoneE164 != nil {
		coachPhone = *coachUser.PhoneE164
	}

	if err := s.notifier.EnqueuePaymentFailedAlert(
		ctx,
		coach.ID, client.ID,
		coachUser.FullName, coachUser.Email, coachPhone,
		clientUser.FullName,
		p.BillingYear, p.BillingMonth, p.Provider,
	); err != nil {
		s.logger.WarnContext(ctx, "billing: enqueue payment failed alert", "payment_id", p.ID, "error", err)
	}
}

// HandleStripeWebhook processes a verified Stripe event and updates payment status.
func (s *Service) HandleStripeWebhook(ctx context.Context, rawBody []byte, sigHeader string) error {
	event, err := s.stripe.VerifyWebhookSignature(rawBody, sigHeader)
	if err != nil {
		return fmt.Errorf("billing: stripe webhook verify: %w", err)
	}

	if err := s.repo.RecordWebhookEvent(ctx, ProviderStripe, event.ID, rawBody); errors.Is(err, ErrDuplicateWebhook) {
		s.logger.InfoContext(ctx, "duplicate stripe webhook, skipping", "event_id", event.ID)
		return nil
	}

	switch event.Type {
	case "payment_intent.succeeded":
		var pi struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(event.Data.Raw, &pi); err == nil {
			p, err := s.repo.GetPaymentByProviderRef(ctx, ProviderStripe, pi.ID)
			if err == nil {
				_ = s.repo.UpdatePaymentStatus(ctx, p.ID, PaymentStatusPaid, &pi.ID)
			}
		}

	case "payment_intent.payment_failed":
		var pi struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(event.Data.Raw, &pi); err == nil {
			p, err := s.repo.GetPaymentByProviderRef(ctx, ProviderStripe, pi.ID)
			if err == nil {
				// FRONTEND: surface failed payment in coach dashboard so they can follow up
				_ = s.repo.UpdatePaymentStatus(ctx, p.ID, PaymentStatusFailed, &pi.ID)
				s.logger.WarnContext(ctx, "stripe payment failed", "payment_id", p.ID, "client_id", p.ClientID)
				s.notifyPaymentFailed(ctx, p)
			}
		}

	case "invoice.payment_succeeded":
		if s.subSvc != nil {
			var inv struct {
				Subscription        string `json:"subscription"`
				PeriodStart         int64  `json:"period_start"`
				PeriodEnd           int64  `json:"period_end"`
				BillingReason       string `json:"billing_reason"`
			}
			if err := json.Unmarshal(event.Data.Raw, &inv); err == nil && inv.Subscription != "" {
				// Skip the first invoice since credits are granted on AssignPlan.
				if inv.BillingReason != "subscription_create" {
					periodStart := timeFromUnix(inv.PeriodStart)
					periodEnd := timeFromUnix(inv.PeriodEnd)
					if err := s.subSvc.HandleInvoiceSucceeded(ctx, inv.Subscription, periodStart, periodEnd); err != nil {
						s.logger.WarnContext(ctx, "billing: invoice.payment_succeeded handler", "error", err)
					}
				}
			}
		}

	case "customer.subscription.deleted":
		if s.subSvc != nil {
			var stripeSubObj struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(event.Data.Raw, &stripeSubObj); err == nil {
				if err := s.subSvc.HandleSubscriptionDeleted(ctx, stripeSubObj.ID); err != nil {
					s.logger.WarnContext(ctx, "billing: customer.subscription.deleted handler", "error", err)
				}
			}
		}
	}

	return nil
}

func timeFromUnix(unix int64) time.Time {
	return time.Unix(unix, 0).UTC()
}

// HandleGoCardlessWebhook processes a verified GoCardless event.
//
// NOT ACTIVE: GoCardless application was rejected. See ErrGoCardlessNotAvailable.
func (s *Service) HandleGoCardlessWebhook(ctx context.Context, rawBody []byte, sigHeader string) error {
	return ErrGoCardlessNotAvailable
	// Preserved implementation — uncomment when GoCardless account is approved:
	// if err := s.gc.VerifyWebhookSignature(rawBody, sigHeader); err != nil {
	// 	return err
	// }
	// var payload struct {
	// 	Events []struct {
	// 		ID           string `json:"id"`
	// 		ResourceType string `json:"resource_type"`
	// 		Action       string `json:"action"`
	// 		Links        struct {
	// 			Payment string `json:"payment"`
	// 			Mandate string `json:"mandate"`
	// 		} `json:"links"`
	// 	} `json:"events"`
	// }
	// if err := parseJSON(rawBody, &payload); err != nil {
	// 	return fmt.Errorf("billing: parse gc webhook: %w", err)
	// }
	// for _, ev := range payload.Events {
	// 	if err := s.repo.RecordWebhookEvent(ctx, ProviderGoCardless, ev.ID, rawBody); errors.Is(err, ErrDuplicateWebhook) {
	// 		continue
	// 	}
	// 	switch ev.ResourceType + "." + ev.Action {
	// 	case "payments.paid_out":
	// 		p, err := s.repo.GetPaymentByProviderRef(ctx, ProviderGoCardless, ev.Links.Payment)
	// 		if err == nil {
	// 			_ = s.repo.UpdatePaymentStatus(ctx, p.ID, PaymentStatusPaid, &ev.Links.Payment)
	// 		}
	// 	case "payments.failed", "payments.charged_back":
	// 		p, err := s.repo.GetPaymentByProviderRef(ctx, ProviderGoCardless, ev.Links.Payment)
	// 		if err == nil {
	// 			_ = s.repo.UpdatePaymentStatus(ctx, p.ID, PaymentStatusFailed, &ev.Links.Payment)
	// 			s.logger.WarnContext(ctx, "gocardless payment failed", "payment_id", p.ID, "action", ev.Action)
	// 			s.notifyPaymentFailed(ctx, p)
	// 		}
	// 	case "mandates.cancelled", "mandates.expired":
	// 		s.logger.WarnContext(ctx, "gocardless mandate cancelled/expired", "mandate_id", ev.Links.Mandate)
	// 	}
	// }
	// return nil
}
