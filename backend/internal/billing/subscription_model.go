package billing

import (
	"time"

	"github.com/google/uuid"
)

// Subscription plan change statuses
const (
	PlanChangeStatusPending   = "pending"
	PlanChangeStatusApproved  = "approved"
	PlanChangeStatusRejected  = "rejected"
	PlanChangeStatusCancelled = "cancelled"
)

// Subscription statuses (mirror DB CHECK constraint)
const (
	SubStatusActive     = "active"
	SubStatusPastDue    = "past_due"
	SubStatusCancelled  = "cancelled"
	SubStatusPaused     = "paused"
	SubStatusIncomplete = "incomplete"
)

// Ledger reason constants
const (
	LedgerReasonRenewal        = "subscription_renewal"
	LedgerReasonBooked         = "session_booked"
	LedgerReasonCancelled      = "session_cancelled"
	LedgerReasonPlanChange     = "plan_change_adjustment"
	LedgerReasonManual         = "manual_adjustment"
)

// SubscriptionPlan is a recurring billing plan created by a coach.
type SubscriptionPlan struct {
	ID               uuid.UUID `json:"id"`
	CoachID          uuid.UUID `json:"coach_id"`
	Name             string    `json:"name"`
	Description      *string   `json:"description,omitempty"`
	SessionsIncluded int       `json:"sessions_included"`
	AmountPence      int       `json:"amount_pence"`
	StripeProductID  *string   `json:"-"`
	StripePriceID    *string   `json:"-"`
	Active           bool      `json:"active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ClientSubscription links a client to a plan and tracks their Stripe subscription.
type ClientSubscription struct {
	ID                    uuid.UUID  `json:"id"`
	ClientID              uuid.UUID  `json:"client_id"`
	PlanID                uuid.UUID  `json:"plan_id"`
	StripeSubscriptionID  *string    `json:"-"`
	StripeCustomerID      *string    `json:"-"`
	Status                string     `json:"status"`
	CurrentPeriodStart    *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd      *time.Time `json:"current_period_end,omitempty"`
	SessionsBalance       int        `json:"sessions_balance"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

// ClientSubscriptionDetail is the coach view: subscription + plan name + sessions balance.
// Does NOT include amount_pence — coaches see that only via SubscriptionPlan directly.
type ClientSubscriptionDetail struct {
	ID                   uuid.UUID  `json:"id"`
	ClientID             uuid.UUID  `json:"client_id"`
	PlanID               uuid.UUID  `json:"plan_id"`
	PlanName             string     `json:"plan_name"`
	SessionsIncluded     int        `json:"sessions_included"`
	Status               string     `json:"status"`
	CurrentPeriodStart   *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd     *time.Time `json:"current_period_end,omitempty"`
	SessionsBalance      int        `json:"sessions_balance"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// PlanChange represents a staged plan change request.
type PlanChange struct {
	ID             uuid.UUID `json:"id"`
	SubscriptionID uuid.UUID `json:"subscription_id"`
	FromPlanID     uuid.UUID `json:"from_plan_id"`
	ToPlanID       uuid.UUID `json:"to_plan_id"`
	RequestedBy    uuid.UUID `json:"requested_by"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// BalanceLedgerEntry records a session balance change.
type BalanceLedgerEntry struct {
	ID             uuid.UUID  `json:"id"`
	ClientID       uuid.UUID  `json:"client_id"`
	SubscriptionID *uuid.UUID `json:"subscription_id,omitempty"`
	Delta          int        `json:"delta"`
	Reason         string     `json:"reason"`
	ReferenceID    *uuid.UUID `json:"reference_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ClientSubscriptionView is the client-safe view.
// No price information is ever included.
type ClientSubscriptionView struct {
	PlanName         string     `json:"plan_name"`
	SessionsBalance  int        `json:"sessions_balance"`
	CurrentPeriodEnd *time.Time `json:"current_period_end,omitempty"`
}

// --- Request/response types ---

// CreatePlanRequest is the body for POST /subscription-plans.
type CreatePlanRequest struct {
	Name             string  `json:"name"              validate:"required,min=2,max=100"`
	Description      *string `json:"description"       validate:"omitempty,max=500"`
	SessionsIncluded int     `json:"sessions_included" validate:"required,min=1,max=100"`
	AmountPence      int     `json:"amount_pence"      validate:"required,min=100"`
}

// UpdatePlanRequest is the body for PUT /subscription-plans/{planID}.
// Amount changes are handled by creating a new Stripe Price — only metadata fields here.
type UpdatePlanRequest struct {
	Name             string  `json:"name"              validate:"required,min=2,max=100"`
	Description      *string `json:"description"       validate:"omitempty,max=500"`
	SessionsIncluded int     `json:"sessions_included" validate:"required,min=1,max=100"`
}

// AssignPlanRequest is the body for POST /clients/{clientID}/subscription.
type AssignPlanRequest struct {
	PlanID string `json:"plan_id" validate:"required,uuid4"`
}

// RequestPlanChangeRequest is the body for POST /clients/{clientID}/subscription/plan-change.
type RequestPlanChangeRequest struct {
	NewPlanID string `json:"new_plan_id" validate:"required,uuid4"`
}
