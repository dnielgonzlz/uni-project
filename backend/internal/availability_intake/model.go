package availability_intake

import (
	"time"

	"github.com/google/uuid"
)

// Conversation states for the Twilio availability intake flow.
const (
	StateIdle                   = "idle"
	StateAwaitingDays           = "awaiting_days"
	StateAwaitingTimes          = "awaiting_times"
	StateAwaitingClarification  = "awaiting_clarification"
	StateComplete               = "complete"
)

const (
	ChannelSMS      = "sms"
	ChannelWhatsApp = "whatsapp"
)

// Conversation holds the Twilio state machine for one client.
type Conversation struct {
	ID        uuid.UUID      `json:"id"`
	ClientID  uuid.UUID      `json:"client_id"`
	State     string         `json:"state"`
	Context   map[string]any `json:"context"` // stores partial parse results
	StartedAt *time.Time     `json:"started_at,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type InboundClient struct {
	ID               uuid.UUID
	AIBookingEnabled bool
}

// InboundMessage is parsed from the Twilio webhook body.
type InboundMessage struct {
	MessageSID string // Twilio MessageSid, used for webhook idempotency
	From       string // E.164 phone number of sender after channel normalisation
	Body       string // message text
	Channel    string // "sms" | "whatsapp"
}
