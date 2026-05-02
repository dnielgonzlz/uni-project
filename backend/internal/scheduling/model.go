package scheduling

import (
	"time"

	"github.com/google/uuid"
)

// Session statuses
const (
	StatusProposed            = "proposed"
	StatusConfirmed           = "confirmed"
	StatusCancelled           = "cancelled"
	StatusCompleted           = "completed"
	StatusPendingCancellation = "pending_cancellation"
)

// ScheduleRun statuses
const (
	RunPendingConfirmation = "pending_confirmation"
	RunConfirmed           = "confirmed"
	RunRejected            = "rejected"
	RunExpired             = "expired"
)

// Session is a single booked 60-minute training session.
type Session struct {
	ID                        uuid.UUID  `json:"id"`
	CoachID                   uuid.UUID  `json:"coach_id"`
	ClientID                  uuid.UUID  `json:"client_id"`
	ScheduleRunID             *uuid.UUID `json:"schedule_run_id,omitempty"`
	StartsAt                  time.Time  `json:"starts_at"`
	EndsAt                    time.Time  `json:"ends_at"`
	Status                    string     `json:"status"`
	Notes                     *string    `json:"notes,omitempty"`
	CancellationReason        *string    `json:"cancellation_reason,omitempty"`
	CancellationRequestedAt   *time.Time `json:"cancellation_requested_at,omitempty"`
	ClientName                *string    `json:"client_name,omitempty"`
	CoachName                 *string    `json:"coach_name,omitempty"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

// ScheduleRun represents one invocation of the OR-Tools solver for a coach's week.
type ScheduleRun struct {
	ID           uuid.UUID  `json:"id"`
	CoachID      uuid.UUID  `json:"coach_id"`
	WeekStart    time.Time  `json:"week_start"`
	Status       string     `json:"status"`
	SolverInput  any        `json:"solver_input,omitempty"`
	SolverOutput any        `json:"solver_output,omitempty"`
	ExpiresAt    time.Time  `json:"expires_at"`
	Sessions     []Session  `json:"sessions,omitempty"` // populated by service layer
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// SessionCredit is issued when a session is cancelled with sufficient notice.
type SessionCredit struct {
	ID              uuid.UUID  `json:"id"`
	ClientID        uuid.UUID  `json:"client_id"`
	Reason          string     `json:"reason"`
	SourceSessionID uuid.UUID  `json:"source_session_id"`
	UsedSessionID   *uuid.UUID `json:"used_session_id,omitempty"`
	ExpiresAt       time.Time  `json:"expires_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

// --- Request/response types ---

// TriggerRunRequest is the body for POST /api/v1/schedule-runs
type TriggerRunRequest struct {
	WeekStart string `json:"week_start" validate:"required"` // "YYYY-MM-DD"
}

// CancelSessionRequest is the body for POST /api/v1/sessions/{id}/cancel
type CancelSessionRequest struct {
	Reason string `json:"reason" validate:"required,max=500"`
}

// CancelSessionResponse is returned from POST /api/v1/sessions/{id}/cancel.
// WithinWindow=true means the cancellation is pending coach approval.
type CancelSessionResponse struct {
	Session      *Session       `json:"session"`
	Credit       *SessionCredit `json:"credit,omitempty"`
	WithinWindow bool           `json:"within_24h_window"`
	Message      string         `json:"message"`
}

// ReviewCancellationRequest is the body for the coach approve/waive endpoints.
type ReviewCancellationRequest struct {
	Notes string `json:"notes" validate:"omitempty,max=500"`
}

// ListSessionsFilter holds query parameters for GET /api/v1/sessions
type ListSessionsFilter struct {
	Status string // optional status filter
}

// ConfirmRunRequest is the optional body for POST /api/v1/schedule-runs/{runID}/confirm
type ConfirmRunRequest struct {
	ExcludedSessionIDs []string `json:"excluded_session_ids"` // UUIDs of sessions to skip (they are cancelled)
}

// UpdateSessionRequest is the body for PUT /api/v1/sessions/{sessionID}
type UpdateSessionRequest struct {
	StartsAt string `json:"starts_at" validate:"required"` // RFC3339
	EndsAt   string `json:"ends_at" validate:"required"`   // RFC3339
}

// --- Solver wire types ---

// SolverRequest is sent to the Python OR-Tools microservice.
type SolverRequest struct {
	WeekStart       string          `json:"week_start"`
	Coach           SolverCoach     `json:"coach"`
	Clients         []SolverClient  `json:"clients"`
	ExistingSessions []SolverSession `json:"existing_sessions"`
}

type SolverCoach struct {
	ID                string           `json:"id"`
	WorkingHours      []SolverTimeSlot `json:"working_hours"`
	MaxSessionsPerDay int              `json:"max_sessions_per_day"`
}

type SolverClient struct {
	ID               string           `json:"id"`
	SessionCount     int              `json:"session_count"` // sessions this week
	PriorityScore    int              `json:"priority_score"`
	PreferredWindows []SolverTimeSlot `json:"preferred_windows"`
}

type SolverTimeSlot struct {
	DayOfWeek int    `json:"day_of_week"` // 0=Mon
	StartTime string `json:"start_time"`  // "HH:MM"
	EndTime   string `json:"end_time"`
}

type SolverSession struct {
	ClientID string `json:"client_id"`
	StartsAt string `json:"starts_at"` // RFC3339
	EndsAt   string `json:"ends_at"`
}

// SolverResponse is returned by the Python OR-Tools microservice.
type SolverResponse struct {
	Status              string          `json:"status"` // "optimal" | "feasible" | "infeasible"
	Sessions            []SolverSession `json:"sessions"`
	UnscheduledClients  []string        `json:"unscheduled_clients"`
}
