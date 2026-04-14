package scheduling

import (
	"fmt"
	"time"
)

const (
	// SessionDuration is fixed at 60 minutes per the architecture decision.
	SessionDuration = 60 * time.Minute

	// RecoveryPeriod is the minimum rest time between any two sessions for the same client.
	RecoveryPeriod = 24 * time.Hour

	// MaxSessionsPerDay is the normal daily cap for a coach.
	MaxSessionsPerDay = 4

	// MaxSessionsPerDayException is the absolute hard cap (requires explicit exception).
	MaxSessionsPerDayException = 5

	// CancellationNoticeWindow is the minimum advance notice required to earn a session credit.
	CancellationNoticeWindow = 24 * time.Hour
)

// ConstraintError is returned when a hard scheduling constraint is violated.
type ConstraintError struct {
	Code    string
	Message string
}

func (e *ConstraintError) Error() string {
	return fmt.Sprintf("constraint violation [%s]: %s", e.Code, e.Message)
}

// CheckRecoveryPeriod returns an error if newStart is within RecoveryPeriod
// of any existing session for the same client.
//
// existing must be the list of confirmed/proposed sessions for the client,
// sorted by starts_at ascending.
func CheckRecoveryPeriod(newStart time.Time, existing []Session) error {
	newEnd := newStart.Add(SessionDuration)

	for _, s := range existing {
		// Gap before the new session starts
		if newStart.Before(s.StartsAt) {
			gap := s.StartsAt.Sub(newEnd)
			if gap < RecoveryPeriod {
				return &ConstraintError{
					Code: "recovery_period",
					Message: fmt.Sprintf(
						"session at %s is only %s before the proposed slot — minimum 24h required",
						s.StartsAt.Format(time.RFC3339), gap.Round(time.Minute),
					),
				}
			}
		}

		// Gap after the existing session ends
		if newStart.After(s.StartsAt) {
			gap := newStart.Sub(s.EndsAt)
			if gap < RecoveryPeriod {
				return &ConstraintError{
					Code: "recovery_period",
					Message: fmt.Sprintf(
						"session at %s ends only %s before the proposed slot — minimum 24h required",
						s.StartsAt.Format(time.RFC3339), gap.Round(time.Minute),
					),
				}
			}
		}
	}
	return nil
}

// CheckDailyLimit returns an error if adding a session at newStart would exceed
// the daily cap for the coach.
//
// daySessions must be all confirmed/proposed sessions for the coach on the same calendar day.
// allowException permits up to MaxSessionsPerDayException if true.
func CheckDailyLimit(newStart time.Time, daySessions []Session, allowException bool) error {
	limit := MaxSessionsPerDay
	if allowException {
		limit = MaxSessionsPerDayException
	}

	if len(daySessions) >= limit {
		return &ConstraintError{
			Code: "daily_limit",
			Message: fmt.Sprintf(
				"coach already has %d sessions on %s (limit: %d)",
				len(daySessions), newStart.Format("2006-01-02"), limit,
			),
		}
	}
	return nil
}

// CheckWithinWorkingHours returns an error if [start, start+60min) falls outside
// any of the coach's working hour slots for that day of the week.
//
// workingHours is the list of WorkingHours for the coach.
func CheckWithinWorkingHours(start time.Time, workingHours []WorkingHoursSlot) error {
	// Convert to London local time for day-of-week and hour comparison.
	loc, _ := time.LoadLocation("Europe/London")
	local := start.In(loc)
	end := local.Add(SessionDuration)

	// ISO weekday: Monday=0 … Sunday=6
	dow := int(local.Weekday()+6) % 7

	sessionStartMins := local.Hour()*60 + local.Minute()
	sessionEndMins := end.Hour()*60 + end.Minute()

	for _, wh := range workingHours {
		if wh.DayOfWeek != dow {
			continue
		}
		whStartMins := wh.StartHour*60 + wh.StartMinute
		whEndMins := wh.EndHour*60 + wh.EndMinute

		if sessionStartMins >= whStartMins && sessionEndMins <= whEndMins {
			return nil // within this slot
		}
	}

	return &ConstraintError{
		Code: "outside_working_hours",
		Message: fmt.Sprintf(
			"proposed time %s is outside the coach's working hours",
			start.In(loc).Format("Mon 15:04"),
		),
	}
}

// WorkingHoursSlot is a pre-parsed version of availability.WorkingHours
// used in constraint checks (avoids importing the availability package here).
type WorkingHoursSlot struct {
	DayOfWeek   int // 0=Mon
	StartHour   int
	StartMinute int
	EndHour     int
	EndMinute   int
}

// CancellationEarnsCredit returns true when the session is still far enough away
// that the client should receive a session credit.
func CancellationEarnsCredit(sessionStart, now time.Time) bool {
	return sessionStart.Sub(now) >= CancellationNoticeWindow
}
