package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors for subscription operations.
var (
	ErrPlanNotFound              = errors.New("subscription plan not found")
	ErrSubscriptionNotFound      = errors.New("subscription not found")
	ErrSubscriptionAlreadyExists = errors.New("client already has a subscription")
	ErrNoCardOnFile              = errors.New("no payment card on file for this client")
	ErrPlanChangeNotPending      = errors.New("plan change is not in pending status")
	ErrNoSessionBalance          = errors.New("client has no session balance")
)

// SubscriptionRepository handles DB operations for subscription billing.
type SubscriptionRepository struct {
	db *pgxpool.Pool
}

func NewSubscriptionRepository(db *pgxpool.Pool) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

// --- Plans ---

func (r *SubscriptionRepository) CreatePlan(ctx context.Context, plan *SubscriptionPlan) (*SubscriptionPlan, error) {
	const q = `
		INSERT INTO subscription_plans
		  (coach_id, name, description, sessions_included, amount_pence, stripe_product_id, stripe_price_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, coach_id, name, description, sessions_included, amount_pence,
		          stripe_product_id, stripe_price_id, active, created_at, updated_at`

	row := r.db.QueryRow(ctx, q,
		plan.CoachID, plan.Name, plan.Description, plan.SessionsIncluded,
		plan.AmountPence, plan.StripeProductID, plan.StripePriceID,
	)
	return scanPlan(row)
}

func (r *SubscriptionRepository) ListPlansByCoach(ctx context.Context, coachID uuid.UUID) ([]SubscriptionPlan, error) {
	const q = `
		SELECT id, coach_id, name, description, sessions_included, amount_pence,
		       stripe_product_id, stripe_price_id, active, created_at, updated_at
		FROM subscription_plans
		WHERE coach_id = $1
		ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, q, coachID)
	if err != nil {
		return nil, fmt.Errorf("billing: list plans: %w", err)
	}
	defer rows.Close()

	var result []SubscriptionPlan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *p)
	}
	return result, rows.Err()
}

func (r *SubscriptionRepository) GetPlanByID(ctx context.Context, id uuid.UUID) (*SubscriptionPlan, error) {
	const q = `
		SELECT id, coach_id, name, description, sessions_included, amount_pence,
		       stripe_product_id, stripe_price_id, active, created_at, updated_at
		FROM subscription_plans WHERE id = $1`

	row := r.db.QueryRow(ctx, q, id)
	p, err := scanPlan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlanNotFound
	}
	return p, err
}

func (r *SubscriptionRepository) UpdatePlan(ctx context.Context, plan *SubscriptionPlan) (*SubscriptionPlan, error) {
	const q = `
		UPDATE subscription_plans
		SET name = $2, description = $3, sessions_included = $4, updated_at = NOW()
		WHERE id = $1
		RETURNING id, coach_id, name, description, sessions_included, amount_pence,
		          stripe_product_id, stripe_price_id, active, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, plan.ID, plan.Name, plan.Description, plan.SessionsIncluded)
	p, err := scanPlan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlanNotFound
	}
	return p, err
}

func (r *SubscriptionRepository) ArchivePlan(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE subscription_plans SET active = FALSE, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id)
	return err
}

// --- Subscriptions ---

func (r *SubscriptionRepository) CreateSubscription(ctx context.Context, sub *ClientSubscription) (*ClientSubscription, error) {
	const q = `
		INSERT INTO client_subscriptions
		  (client_id, plan_id, stripe_subscription_id, stripe_customer_id, status,
		   current_period_start, current_period_end)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, client_id, plan_id, stripe_subscription_id, stripe_customer_id, status,
		          current_period_start, current_period_end, sessions_balance, created_at, updated_at`

	row := r.db.QueryRow(ctx, q,
		sub.ClientID, sub.PlanID, sub.StripeSubscriptionID, sub.StripeCustomerID,
		sub.Status, sub.CurrentPeriodStart, sub.CurrentPeriodEnd,
	)
	result, err := scanSubscription(row)
	if err != nil {
		// Check for unique constraint violation on client_id
		if isUniqueViolation(err) {
			return nil, ErrSubscriptionAlreadyExists
		}
		return nil, err
	}
	return result, nil
}

func (r *SubscriptionRepository) GetSubscriptionByClientID(ctx context.Context, clientID uuid.UUID) (*ClientSubscription, error) {
	const q = `
		SELECT id, client_id, plan_id, stripe_subscription_id, stripe_customer_id, status,
		       current_period_start, current_period_end, sessions_balance, created_at, updated_at
		FROM client_subscriptions WHERE client_id = $1`

	row := r.db.QueryRow(ctx, q, clientID)
	s, err := scanSubscription(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSubscriptionNotFound
	}
	return s, err
}

func (r *SubscriptionRepository) GetSubscriptionByID(ctx context.Context, id uuid.UUID) (*ClientSubscription, error) {
	const q = `
		SELECT id, client_id, plan_id, stripe_subscription_id, stripe_customer_id, status,
		       current_period_start, current_period_end, sessions_balance, created_at, updated_at
		FROM client_subscriptions WHERE id = $1`

	row := r.db.QueryRow(ctx, q, id)
	s, err := scanSubscription(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSubscriptionNotFound
	}
	return s, err
}

func (r *SubscriptionRepository) GetSubscriptionByStripeID(ctx context.Context, stripeSubID string) (*ClientSubscription, error) {
	const q = `
		SELECT id, client_id, plan_id, stripe_subscription_id, stripe_customer_id, status,
		       current_period_start, current_period_end, sessions_balance, created_at, updated_at
		FROM client_subscriptions WHERE stripe_subscription_id = $1`

	row := r.db.QueryRow(ctx, q, stripeSubID)
	s, err := scanSubscription(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSubscriptionNotFound
	}
	return s, err
}

func (r *SubscriptionRepository) UpdateSubscriptionStatus(ctx context.Context, id uuid.UUID, status string, periodStart, periodEnd *string) error {
	const q = `
		UPDATE client_subscriptions
		SET status = $2,
		    current_period_start = COALESCE($3::TIMESTAMPTZ, current_period_start),
		    current_period_end   = COALESCE($4::TIMESTAMPTZ, current_period_end),
		    updated_at = NOW()
		WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id, status, periodStart, periodEnd)
	return err
}

func (r *SubscriptionRepository) UpdateSubscriptionPlan(ctx context.Context, id, newPlanID uuid.UUID) error {
	const q = `UPDATE client_subscriptions SET plan_id = $2, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id, newPlanID)
	return err
}

func (r *SubscriptionRepository) CancelSubscription(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE client_subscriptions SET status = 'cancelled', updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id)
	return err
}

// AddSessionBalance updates sessions_balance and inserts a ledger entry atomically.
// The CHECK (sessions_balance >= 0) constraint naturally rejects negative deltas that would go below 0.
func (r *SubscriptionRepository) AddSessionBalance(ctx context.Context, subscriptionID, clientID uuid.UUID, delta int, reason string, referenceID *uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("billing: begin add session balance tx: %w", err)
	}
	defer tx.Rollback(ctx)

	const updateQ = `
		UPDATE client_subscriptions
		SET sessions_balance = sessions_balance + $2, updated_at = NOW()
		WHERE id = $1`
	if _, err := tx.Exec(ctx, updateQ, subscriptionID, delta); err != nil {
		return fmt.Errorf("billing: update sessions_balance: %w", err)
	}

	const ledgerQ = `
		INSERT INTO session_balance_ledger (client_id, subscription_id, delta, reason, reference_id)
		VALUES ($1, $2, $3, $4, $5)`
	if _, err := tx.Exec(ctx, ledgerQ, clientID, subscriptionID, delta, reason, referenceID); err != nil {
		return fmt.Errorf("billing: insert ledger entry: %w", err)
	}

	return tx.Commit(ctx)
}

// DeductSession decrements sessions_balance by 1. Returns ErrNoSessionBalance if balance is 0.
func (r *SubscriptionRepository) DeductSession(ctx context.Context, clientID uuid.UUID, sessionID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("billing: begin deduct session tx: %w", err)
	}
	defer tx.Rollback(ctx)

	const checkQ = `SELECT id, sessions_balance FROM client_subscriptions WHERE client_id = $1`
	var subID uuid.UUID
	var balance int
	if err := tx.QueryRow(ctx, checkQ, clientID).Scan(&subID, &balance); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSubscriptionNotFound
		}
		return fmt.Errorf("billing: check balance: %w", err)
	}
	if balance <= 0 {
		return ErrNoSessionBalance
	}

	const updateQ = `
		UPDATE client_subscriptions
		SET sessions_balance = sessions_balance - 1, updated_at = NOW()
		WHERE id = $1`
	if _, err := tx.Exec(ctx, updateQ, subID); err != nil {
		return fmt.Errorf("billing: deduct session balance: %w", err)
	}

	refID := sessionID
	const ledgerQ = `
		INSERT INTO session_balance_ledger (client_id, subscription_id, delta, reason, reference_id)
		VALUES ($1, $2, -1, 'session_booked', $3)`
	if _, err := tx.Exec(ctx, ledgerQ, clientID, subID, refID); err != nil {
		return fmt.Errorf("billing: insert deduct ledger: %w", err)
	}

	return tx.Commit(ctx)
}

// --- Plan changes ---

func (r *SubscriptionRepository) CreatePlanChange(ctx context.Context, change *PlanChange) (*PlanChange, error) {
	const q = `
		INSERT INTO subscription_plan_changes
		  (subscription_id, from_plan_id, to_plan_id, requested_by, status)
		VALUES ($1, $2, $3, $4, 'pending')
		RETURNING id, subscription_id, from_plan_id, to_plan_id, requested_by, status, created_at, updated_at`

	row := r.db.QueryRow(ctx, q,
		change.SubscriptionID, change.FromPlanID, change.ToPlanID, change.RequestedBy,
	)
	return scanPlanChange(row)
}

func (r *SubscriptionRepository) GetPlanChangeByID(ctx context.Context, id uuid.UUID) (*PlanChange, error) {
	const q = `
		SELECT id, subscription_id, from_plan_id, to_plan_id, requested_by, status, created_at, updated_at
		FROM subscription_plan_changes WHERE id = $1`

	row := r.db.QueryRow(ctx, q, id)
	c, err := scanPlanChange(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("billing: plan change not found: %s", id)
	}
	return c, err
}

func (r *SubscriptionRepository) ListPendingPlanChanges(ctx context.Context, coachID uuid.UUID) ([]PlanChange, error) {
	const q = `
		SELECT pc.id, pc.subscription_id, pc.from_plan_id, pc.to_plan_id,
		       pc.requested_by, pc.status, pc.created_at, pc.updated_at
		FROM subscription_plan_changes pc
		JOIN client_subscriptions cs ON cs.id = pc.subscription_id
		JOIN subscription_plans sp ON sp.id = cs.plan_id
		WHERE sp.coach_id = $1 AND pc.status = 'pending'
		ORDER BY pc.created_at ASC`

	rows, err := r.db.Query(ctx, q, coachID)
	if err != nil {
		return nil, fmt.Errorf("billing: list pending plan changes: %w", err)
	}
	defer rows.Close()

	var result []PlanChange
	for rows.Next() {
		c, err := scanPlanChange(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *c)
	}
	return result, rows.Err()
}

func (r *SubscriptionRepository) UpdatePlanChangeStatus(ctx context.Context, id uuid.UUID, status string) error {
	const q = `
		UPDATE subscription_plan_changes
		SET status = $2, updated_at = NOW()
		WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id, status)
	return err
}

// --- Stripe customer mapping ---

func (r *SubscriptionRepository) SetStripeCustomerID(ctx context.Context, clientID uuid.UUID, customerID string) error {
	const q = `UPDATE clients SET stripe_customer_id = $2 WHERE id = $1`
	_, err := r.db.Exec(ctx, q, clientID, customerID)
	return err
}

func (r *SubscriptionRepository) GetStripeCustomerID(ctx context.Context, clientID uuid.UUID) (string, error) {
	const q = `SELECT stripe_customer_id FROM clients WHERE id = $1 AND deleted_at IS NULL`
	var customerID *string
	if err := r.db.QueryRow(ctx, q, clientID).Scan(&customerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNoCardOnFile
		}
		return "", fmt.Errorf("billing: get stripe customer id: %w", err)
	}
	if customerID == nil || *customerID == "" {
		return "", ErrNoCardOnFile
	}
	return *customerID, nil
}

// GetSubscriptionDetailByClientID returns a joined view for the coach.
func (r *SubscriptionRepository) GetSubscriptionDetailByClientID(ctx context.Context, clientID uuid.UUID) (*ClientSubscriptionDetail, error) {
	const q = `
		SELECT cs.id, cs.client_id, cs.plan_id, sp.name, sp.sessions_included,
		       cs.status, cs.current_period_start, cs.current_period_end,
		       cs.sessions_balance, cs.created_at, cs.updated_at
		FROM client_subscriptions cs
		JOIN subscription_plans sp ON sp.id = cs.plan_id
		WHERE cs.client_id = $1`

	var d ClientSubscriptionDetail
	err := r.db.QueryRow(ctx, q, clientID).Scan(
		&d.ID, &d.ClientID, &d.PlanID, &d.PlanName, &d.SessionsIncluded,
		&d.Status, &d.CurrentPeriodStart, &d.CurrentPeriodEnd,
		&d.SessionsBalance, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSubscriptionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("billing: get subscription detail: %w", err)
	}
	return &d, nil
}

// --- Scan helpers ---

func scanPlan(row interface{ Scan(...any) error }) (*SubscriptionPlan, error) {
	var p SubscriptionPlan
	err := row.Scan(
		&p.ID, &p.CoachID, &p.Name, &p.Description, &p.SessionsIncluded, &p.AmountPence,
		&p.StripeProductID, &p.StripePriceID, &p.Active, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("billing: scan plan: %w", err)
	}
	return &p, nil
}

func scanSubscription(row interface{ Scan(...any) error }) (*ClientSubscription, error) {
	var s ClientSubscription
	err := row.Scan(
		&s.ID, &s.ClientID, &s.PlanID, &s.StripeSubscriptionID, &s.StripeCustomerID,
		&s.Status, &s.CurrentPeriodStart, &s.CurrentPeriodEnd,
		&s.SessionsBalance, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("billing: scan subscription: %w", err)
	}
	return &s, nil
}

func scanPlanChange(row interface{ Scan(...any) error }) (*PlanChange, error) {
	var c PlanChange
	err := row.Scan(
		&c.ID, &c.SubscriptionID, &c.FromPlanID, &c.ToPlanID,
		&c.RequestedBy, &c.Status, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("billing: scan plan change: %w", err)
	}
	return &c, nil
}

// isUniqueViolation detects PostgreSQL unique constraint violation errors.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return containsCode(err, "23505")
}

func containsCode(err error, code string) bool {
	type pgErr interface {
		SQLState() string
	}
	if pe, ok := err.(pgErr); ok {
		return pe.SQLState() == code
	}
	return false
}
