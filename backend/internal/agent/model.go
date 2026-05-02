package agent

import (
	"time"

	"github.com/google/uuid"
)

const (
	TemplateStatusMissing  = "missing"
	TemplateStatusPending  = "pending"
	TemplateStatusApproved = "approved"
	TemplateStatusRejected = "rejected"
)

// Settings stores the MVP configuration for the coach's AI booking agent.
type Settings struct {
	CoachID                  uuid.UUID `json:"coach_id"`
	Enabled                  bool      `json:"enabled"`
	TemplateSID              *string   `json:"template_sid,omitempty"`
	TemplateStatus           string    `json:"template_status"`
	PromptDay                string    `json:"prompt_day"`
	PromptTime               string    `json:"prompt_time"`
	Timezone                 string    `json:"timezone"`
	RequireCoachConfirmation bool      `json:"require_coach_confirmation"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

// AgentClient is a coach-owned client row exposed to the allowlist UI.
type AgentClient struct {
	ClientID         uuid.UUID `json:"client_id"`
	FullName         string    `json:"full_name"`
	Email            string    `json:"email"`
	Phone            *string   `json:"phone,omitempty"`
	AIBookingEnabled bool      `json:"ai_booking_enabled"`
}

type UpdateSettingsRequest struct {
	Enabled     bool    `json:"enabled"`
	TemplateSID *string `json:"template_sid" validate:"omitempty"`
}

type UpdateAgentClientRequest struct {
	Enabled bool `json:"enabled"`
}

type TemplateStatusResponse struct {
	TemplateStatus  string  `json:"template_status"`
	RejectionReason *string `json:"rejection_reason,omitempty"`
}

type CampaignClient struct {
	ClientID uuid.UUID
	FullName string
	Phone    string
}

type CampaignCoach struct {
	CoachID     uuid.UUID
	TemplateSID string
}

type CampaignOverview struct {
	CampaignStatus string     `json:"campaign_status"`
	WeekStart      *time.Time `json:"week_start,omitempty"`
	TextedCount    int        `json:"texted_count"`
	RepliedCount   int        `json:"replied_count"`
	WaitingCount   int        `json:"waiting_count"`
	ParsedCount    int        `json:"parsed_count"`
}
