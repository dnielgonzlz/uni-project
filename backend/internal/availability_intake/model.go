package availability_intake

import (
	"time"

	"github.com/google/uuid"
)

// Conversation states for the SMS availability intake flow.
const (
	StateIdle          = "idle"
	StateAwaitingDays  = "awaiting_days"
	StateAwaitingTimes = "awaiting_times"
	StateComplete      = "complete"
)

// Conversation holds the SMS state machine for one client.
type Conversation struct {
	ID        uuid.UUID         `json:"id"`
	ClientID  uuid.UUID         `json:"client_id"`
	State     string            `json:"state"`
	Context   map[string]any    `json:"context"` // stores partial parse results
	StartedAt *time.Time        `json:"started_at,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// InboundSMS is parsed from the Twilio webhook body.
type InboundSMS struct {
	From string // E.164 phone number of sender
	Body string // message text
}
