package availability

import (
	"time"

	"github.com/google/uuid"
)

// WorkingHours represents one day-slot of a coach's availability.
type WorkingHours struct {
	ID         uuid.UUID `json:"id"`
	CoachID    uuid.UUID `json:"coach_id"`
	DayOfWeek  int       `json:"day_of_week"` // 0=Monday … 6=Sunday (ISO week)
	StartTime  string    `json:"start_time"`  // "HH:MM" in Europe/London local time
	EndTime    string    `json:"end_time"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// PreferredWindow represents one time slot a client prefers to train.
type PreferredWindow struct {
	ID          uuid.UUID `json:"id"`
	ClientID    uuid.UUID `json:"client_id"`
	DayOfWeek   int       `json:"day_of_week"`
	StartTime   string    `json:"start_time"`
	EndTime     string    `json:"end_time"`
	Source      string    `json:"source"`       // "manual" | "sms" | "whatsapp"
	CollectedAt time.Time `json:"collected_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// WorkingHoursEntry is one item inside SetWorkingHoursRequest.
type WorkingHoursEntry struct {
	DayOfWeek int    `json:"day_of_week" validate:"min=0,max=6"`
	StartTime string `json:"start_time"  validate:"required"`
	EndTime   string `json:"end_time"    validate:"required"`
}

// SetWorkingHoursRequest replaces the entire working hours set for a coach.
// Sending an empty slice clears all working hours.
type SetWorkingHoursRequest struct {
	Hours []WorkingHoursEntry `json:"hours" validate:"required,dive"`
}

// PreferredWindowEntry is one item inside SetPreferencesRequest.
type PreferredWindowEntry struct {
	DayOfWeek int    `json:"day_of_week" validate:"min=0,max=6"`
	StartTime string `json:"start_time"  validate:"required"`
	EndTime   string `json:"end_time"    validate:"required"`
}

// SetPreferencesRequest replaces all preferred windows for a client.
type SetPreferencesRequest struct {
	Windows []PreferredWindowEntry `json:"windows" validate:"required,dive"`
}
