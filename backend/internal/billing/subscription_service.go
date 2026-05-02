package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// coachLookup is the narrow interface needed to resolve coach IDs from user IDs.
type coachLookup interface {
	GetCoachByUserID(ctx context.Context, userID uuid.UUID) (*users.Coach, error)
	GetClientByID(ctx context.Context, id uuid.UUID) (*users.Client, error)
	GetClientByUserID(ctx context.Context, userID uuid.UUID) (*users.Client, error)
}

// SubscriptionService orchestrates subscription plan and billing operations.
type SubscriptionService struct {
	repo   *SubscriptionRepository
	stripe *StripeClient
	users  coachLookup
	logger *slog.Logger
}

func NewSubscriptionService(repo *SubscriptionRepository, stripe *StripeClient, users coachLookup, logger *slog.Logger) *SubscriptionService {
	return &SubscriptionService{repo: repo, stripe: stripe, users: users, logger: logger}
}

// --- Plans ---

func (s *SubscriptionService) CreatePlan(ctx context.Context, coachUserID uuid.UUID, req CreatePlanRequest) (*SubscriptionPlan, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("subscription: create plan — resolve coach: %w", err)
	}

	desc := ""
	if req.Description != nil {
		desc = *req.Description
	}

	productID, err := s.stripe.CreateProduct(ctx, req.Name, desc)
	if err != nil {
		return nil, fmt.Errorf("subscription: create plan — stripe product: %w", err)
	}

	priceID, err := s.stripe.CreatePrice(ctx, productID, req.AmountPence)
	if err != nil {
		return nil, fmt.Errorf("subscription: create plan — stripe price: %w", err)
	}

	plan := &SubscriptionPlan{
		CoachID:          coach.ID,
		Name:             req.Name,
		Description:      req.Description,
		SessionsIncluded: req.SessionsIncluded,
		AmountPence:      req.AmountPence,
		StripeProductID:  &productID,
		StripePriceID:    &priceID,
	}

	created, err := s.repo.CreatePlan(ctx, plan)
	if err != nil {
		return nil, fmt.Errorf("subscription: create plan: %w", err)
	}
	return created, nil
}

func (s *SubscriptionService) ListPlans(ctx context.Context, coachUserID uuid.UUID) ([]SubscriptionPlan, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("subscription: list plans — resolve coach: %w", err)
	}
	return s.repo.ListPlansByCoach(ctx, coach.ID)
}

func (s *SubscriptionService) UpdatePlan(ctx context.Context, coachUserID, planID uuid.UUID, req UpdatePlanRequest) (*SubscriptionPlan, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("subscription: update plan — resolve coach: %w", err)
	}

	plan, err := s.repo.GetPlanByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	if plan.CoachID != coach.ID {
		return nil, ErrPlanNotFound
	}

	plan.Name = req.Name
	plan.Description = req.Description
	plan.SessionsIncluded = req.SessionsIncluded

	return s.repo.UpdatePlan(ctx, plan)
}

func (s *SubscriptionService) ArchivePlan(ctx context.Context, coachUserID, planID uuid.UUID) error {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return fmt.Errorf("subscription: archive plan — resolve coach: %w", err)
	}

	plan, err := s.repo.GetPlanByID(ctx, planID)
	if err != nil {
		return err
	}
	if plan.CoachID != coach.ID {
		return ErrPlanNotFound
	}

	if plan.StripeProductID != nil {
		_ = s.stripe.ArchiveProduct(ctx, *plan.StripeProductID)
	}
	if plan.StripePriceID != nil {
		_ = s.stripe.ArchivePrice(ctx, *plan.StripePriceID)
	}

	return s.repo.ArchivePlan(ctx, planID)
}

// --- Subscriptions ---

func (s *SubscriptionService) AssignPlan(ctx context.Context, coachUserID, clientID, planID uuid.UUID) (*ClientSubscription, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("subscription: assign plan — resolve coach: %w", err)
	}

	client, err := s.users.GetClientByID(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("subscription: assign plan — resolve client: %w", err)
	}
	if client.CoachID != coach.ID {
		return nil, ErrPlanNotFound
	}

	plan, err := s.repo.GetPlanByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	if plan.CoachID != coach.ID || !plan.Active {
		return nil, ErrPlanNotFound
	}
	if plan.StripePriceID == nil {
		return nil, fmt.Errorf("subscription: plan has no Stripe price")
	}

	customerID, err := s.repo.GetStripeCustomerID(ctx, clientID)
	if err != nil {
		return nil, err // ErrNoCardOnFile if not set
	}

	pmID, err := s.stripe.GetDefaultPaymentMethod(ctx, customerID)
	if err != nil {
		return nil, ErrNoCardOnFile
	}

	result, err := s.stripe.CreateSubscription(ctx, customerID, *plan.StripePriceID, pmID)
	if err != nil {
		return nil, fmt.Errorf("subscription: assign plan — stripe: %w", err)
	}

	periodStart := result.PeriodStart
	periodEnd := result.PeriodEnd

	created, err := s.repo.CreateSubscription(ctx, &ClientSubscription{
		ClientID:             clientID,
		PlanID:               planID,
		StripeSubscriptionID: &result.SubscriptionID,
		StripeCustomerID:     &customerID,
		Status:               result.Status,
		CurrentPeriodStart:   &periodStart,
		CurrentPeriodEnd:     &periodEnd,
	})
	if err != nil {
		return nil, fmt.Errorf("subscription: assign plan — store: %w", err)
	}

	// Grant the first period's session credits immediately.
	if err := s.repo.AddSessionBalance(ctx, created.ID, clientID, plan.SessionsIncluded, LedgerReasonRenewal, nil); err != nil {
		s.logger.WarnContext(ctx, "subscription: assign plan — grant initial credits failed", "subscription_id", created.ID, "error", err)
	}

	return created, nil
}

func (s *SubscriptionService) GetClientSubscriptionDetail(ctx context.Context, coachUserID, clientID uuid.UUID) (*ClientSubscriptionDetail, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("subscription: get detail — resolve coach: %w", err)
	}

	client, err := s.users.GetClientByID(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("subscription: get detail — resolve client: %w", err)
	}
	if client.CoachID != coach.ID {
		return nil, ErrSubscriptionNotFound
	}

	return s.repo.GetSubscriptionDetailByClientID(ctx, clientID)
}

func (s *SubscriptionService) GetClientSubscriptionView(ctx context.Context, clientUserID uuid.UUID) (*ClientSubscriptionView, error) {
	client, err := s.users.GetClientByUserID(ctx, clientUserID)
	if err != nil {
		return nil, fmt.Errorf("subscription: get view — resolve client: %w", err)
	}

	detail, err := s.repo.GetSubscriptionDetailByClientID(ctx, client.ID)
	if err != nil {
		return nil, err
	}

	return &ClientSubscriptionView{
		PlanName:         detail.PlanName,
		SessionsBalance:  detail.SessionsBalance,
		CurrentPeriodEnd: detail.CurrentPeriodEnd,
	}, nil
}

func (s *SubscriptionService) CancelClientSubscription(ctx context.Context, coachUserID, clientID uuid.UUID) error {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return fmt.Errorf("subscription: cancel — resolve coach: %w", err)
	}

	client, err := s.users.GetClientByID(ctx, clientID)
	if err != nil {
		return fmt.Errorf("subscription: cancel — resolve client: %w", err)
	}
	if client.CoachID != coach.ID {
		return ErrSubscriptionNotFound
	}

	existing, err := s.repo.GetSubscriptionByClientID(ctx, clientID)
	if err != nil {
		return err
	}

	if existing.StripeSubscriptionID != nil {
		if err := s.stripe.CancelSubscription(ctx, *existing.StripeSubscriptionID); err != nil {
			return fmt.Errorf("subscription: cancel — stripe: %w", err)
		}
	}

	return s.repo.CancelSubscription(ctx, existing.ID)
}

// --- Plan changes ---

func (s *SubscriptionService) RequestPlanChange(ctx context.Context, coachUserID, clientID, newPlanID uuid.UUID) (*PlanChange, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("subscription: request plan change — resolve coach: %w", err)
	}

	client, err := s.users.GetClientByID(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("subscription: request plan change — resolve client: %w", err)
	}
	if client.CoachID != coach.ID {
		return nil, ErrSubscriptionNotFound
	}

	newPlan, err := s.repo.GetPlanByID(ctx, newPlanID)
	if err != nil {
		return nil, err
	}
	if newPlan.CoachID != coach.ID || !newPlan.Active {
		return nil, ErrPlanNotFound
	}

	existing, err := s.repo.GetSubscriptionByClientID(ctx, clientID)
	if err != nil {
		return nil, err
	}

	return s.repo.CreatePlanChange(ctx, &PlanChange{
		SubscriptionID: existing.ID,
		FromPlanID:     existing.PlanID,
		ToPlanID:       newPlanID,
		RequestedBy:    coachUserID,
	})
}

func (s *SubscriptionService) ApprovePlanChange(ctx context.Context, coachUserID, changeID uuid.UUID) (*PlanChange, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("subscription: approve plan change — resolve coach: %w", err)
	}

	change, err := s.repo.GetPlanChangeByID(ctx, changeID)
	if err != nil {
		return nil, err
	}
	if change.Status != PlanChangeStatusPending {
		return nil, ErrPlanChangeNotPending
	}

	existing, err := s.repo.GetSubscriptionByID(ctx, change.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Verify the subscription belongs to this coach.
	currentPlan, err := s.repo.GetPlanByID(ctx, existing.PlanID)
	if err != nil {
		return nil, err
	}
	if currentPlan.CoachID != coach.ID {
		return nil, ErrPlanChangeNotPending
	}

	newPlan, err := s.repo.GetPlanByID(ctx, change.ToPlanID)
	if err != nil {
		return nil, err
	}
	if newPlan.StripePriceID == nil {
		return nil, fmt.Errorf("subscription: new plan has no Stripe price")
	}

	if existing.StripeSubscriptionID != nil {
		if err := s.stripe.UpdateSubscriptionPrice(ctx, *existing.StripeSubscriptionID, *newPlan.StripePriceID); err != nil {
			return nil, fmt.Errorf("subscription: approve — stripe update: %w", err)
		}
	}

	if err := s.repo.UpdateSubscriptionPlan(ctx, existing.ID, change.ToPlanID); err != nil {
		return nil, fmt.Errorf("subscription: approve — update plan: %w", err)
	}

	// Adjust session balance by the difference in sessions between plans.
	delta := newPlan.SessionsIncluded - currentPlan.SessionsIncluded
	if delta != 0 {
		if err := s.repo.AddSessionBalance(ctx, existing.ID, existing.ClientID, delta, LedgerReasonPlanChange, nil); err != nil {
			s.logger.WarnContext(ctx, "subscription: approve — balance adjustment failed", "error", err)
		}
	}

	if err := s.repo.UpdatePlanChangeStatus(ctx, changeID, PlanChangeStatusApproved); err != nil {
		return nil, fmt.Errorf("subscription: approve — mark approved: %w", err)
	}

	change.Status = PlanChangeStatusApproved
	return change, nil
}

func (s *SubscriptionService) RejectPlanChange(ctx context.Context, coachUserID, changeID uuid.UUID) error {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return fmt.Errorf("subscription: reject plan change — resolve coach: %w", err)
	}

	change, err := s.repo.GetPlanChangeByID(ctx, changeID)
	if err != nil {
		return err
	}
	if change.Status != PlanChangeStatusPending {
		return ErrPlanChangeNotPending
	}

	existing, err := s.repo.GetSubscriptionByID(ctx, change.SubscriptionID)
	if err != nil {
		return err
	}
	currentPlan, err := s.repo.GetPlanByID(ctx, existing.PlanID)
	if err != nil {
		return err
	}
	if currentPlan.CoachID != coach.ID {
		return ErrPlanChangeNotPending
	}

	return s.repo.UpdatePlanChangeStatus(ctx, changeID, PlanChangeStatusRejected)
}

func (s *SubscriptionService) ListPendingPlanChanges(ctx context.Context, coachUserID uuid.UUID) ([]PlanChange, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("subscription: list plan changes — resolve coach: %w", err)
	}
	return s.repo.ListPendingPlanChanges(ctx, coach.ID)
}

// --- Session balance (called by scheduling layer) ---

func (s *SubscriptionService) DeductSession(ctx context.Context, clientID, sessionID uuid.UUID) error {
	return s.repo.DeductSession(ctx, clientID, sessionID)
}

func (s *SubscriptionService) RestoreSession(ctx context.Context, clientID, sessionID uuid.UUID) error {
	sub, err := s.repo.GetSubscriptionByClientID(ctx, clientID)
	if err != nil {
		return err
	}
	refID := sessionID
	return s.repo.AddSessionBalance(ctx, sub.ID, clientID, 1, LedgerReasonCancelled, &refID)
}

// --- Webhook handlers ---

func (s *SubscriptionService) HandleInvoiceSucceeded(ctx context.Context, stripeSubID string, periodStart, periodEnd time.Time) error {
	existing, err := s.repo.GetSubscriptionByStripeID(ctx, stripeSubID)
	if err != nil {
		return err
	}

	plan, err := s.repo.GetPlanByID(ctx, existing.PlanID)
	if err != nil {
		return err
	}

	periodStartStr := periodStart.Format(time.RFC3339)
	periodEndStr := periodEnd.Format(time.RFC3339)
	if err := s.repo.UpdateSubscriptionStatus(ctx, existing.ID, SubStatusActive, &periodStartStr, &periodEndStr); err != nil {
		return fmt.Errorf("subscription: invoice succeeded — update status: %w", err)
	}

	if err := s.repo.AddSessionBalance(ctx, existing.ID, existing.ClientID, plan.SessionsIncluded, LedgerReasonRenewal, nil); err != nil {
		return fmt.Errorf("subscription: invoice succeeded — grant credits: %w", err)
	}

	return nil
}

func (s *SubscriptionService) HandleSubscriptionDeleted(ctx context.Context, stripeSubID string) error {
	existing, err := s.repo.GetSubscriptionByStripeID(ctx, stripeSubID)
	if err != nil {
		return err
	}
	return s.repo.CancelSubscription(ctx, existing.ID)
}
