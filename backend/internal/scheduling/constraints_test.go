package scheduling_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/danielgonzalez/pt-scheduler/internal/scheduling"
)

// fixedTime returns a UTC time on a known date for stable tests.
func fixedTime(day, hour, minute int) time.Time {
	return time.Date(2025, 1, day, hour, minute, 0, 0, time.UTC)
}

func session(start time.Time) scheduling.Session {
	return scheduling.Session{
		ID:       uuid.New(),
		StartsAt: start,
		EndsAt:   start.Add(scheduling.SessionDuration),
		Status:   scheduling.StatusConfirmed,
	}
}

// --- Recovery period ---

func TestCheckRecoveryPeriod_NoConflict(t *testing.T) {
	t.Parallel()
	existing := []scheduling.Session{
		session(fixedTime(6, 9, 0)), // Monday 09:00
	}
	// Propose Tuesday 10:00 — different calendar day, fine
	err := scheduling.CheckRecoveryPeriod(fixedTime(7, 10, 0), existing)
	require.NoError(t, err)
}

func TestCheckRecoveryPeriod_SameDay_After(t *testing.T) {
	t.Parallel()
	existing := []scheduling.Session{
		session(fixedTime(6, 9, 0)), // Monday 09:00
	}
	// Propose Monday 20:00 — same calendar day, not allowed
	err := scheduling.CheckRecoveryPeriod(fixedTime(6, 20, 0), existing)
	require.Error(t, err)

	var ce *scheduling.ConstraintError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, "recovery_period", ce.Code)
}

func TestCheckRecoveryPeriod_SameDay_Before(t *testing.T) {
	t.Parallel()
	existing := []scheduling.Session{
		session(fixedTime(7, 15, 0)), // Tuesday 15:00
	}
	// Propose Tuesday 08:00 — same calendar day, not allowed
	err := scheduling.CheckRecoveryPeriod(fixedTime(7, 8, 0), existing)
	require.Error(t, err)
}

func TestCheckRecoveryPeriod_CrossMidnight_Allowed(t *testing.T) {
	t.Parallel()
	existing := []scheduling.Session{
		session(fixedTime(6, 19, 0)), // Monday 19:00
	}
	// Propose Tuesday 08:00 — different calendar day despite being only 13h later
	err := scheduling.CheckRecoveryPeriod(fixedTime(7, 8, 0), existing)
	require.NoError(t, err)
}

// --- Daily limit ---

func TestCheckDailyLimit_UnderLimit(t *testing.T) {
	t.Parallel()
	day := []scheduling.Session{
		session(fixedTime(6, 9, 0)),
		session(fixedTime(6, 11, 0)),
	}
	err := scheduling.CheckDailyLimit(fixedTime(6, 13, 0), day, false)
	require.NoError(t, err)
}

func TestCheckDailyLimit_AtLimit(t *testing.T) {
	t.Parallel()
	day := []scheduling.Session{
		session(fixedTime(6, 9, 0)),
		session(fixedTime(6, 11, 0)),
		session(fixedTime(6, 13, 0)),
		session(fixedTime(6, 15, 0)),
	}
	err := scheduling.CheckDailyLimit(fixedTime(6, 17, 0), day, false)
	require.Error(t, err)
	var ce *scheduling.ConstraintError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, "daily_limit", ce.Code)
}

func TestCheckDailyLimit_ExceptionAllows5(t *testing.T) {
	t.Parallel()
	day := []scheduling.Session{
		session(fixedTime(6, 9, 0)),
		session(fixedTime(6, 11, 0)),
		session(fixedTime(6, 13, 0)),
		session(fixedTime(6, 15, 0)),
	}
	// With exception flag, 5th session is allowed
	err := scheduling.CheckDailyLimit(fixedTime(6, 17, 0), day, true)
	require.NoError(t, err)
}

func TestCheckDailyLimit_ExceptionDoesNotAllow6(t *testing.T) {
	t.Parallel()
	day := make([]scheduling.Session, 5)
	for i := range day {
		day[i] = session(fixedTime(6, 9+i*2, 0))
	}
	err := scheduling.CheckDailyLimit(fixedTime(6, 19, 0), day, true)
	require.Error(t, err)
}

// --- Working hours ---

func TestCheckWithinWorkingHours_Inside(t *testing.T) {
	t.Parallel()
	// Monday (dow=0), 09:00–17:00 working hours in London
	wh := []scheduling.WorkingHoursSlot{
		{DayOfWeek: 0, StartHour: 9, StartMinute: 0, EndHour: 17, EndMinute: 0},
	}
	// Monday 2025-01-06 10:00 UTC (London is UTC in January)
	start := time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC)
	err := scheduling.CheckWithinWorkingHours(start, wh)
	require.NoError(t, err)
}

func TestCheckWithinWorkingHours_Outside_TooLate(t *testing.T) {
	t.Parallel()
	wh := []scheduling.WorkingHoursSlot{
		{DayOfWeek: 0, StartHour: 9, StartMinute: 0, EndHour: 17, EndMinute: 0},
	}
	// Monday 16:30 — session ends at 17:30, outside working hours
	start := time.Date(2025, 1, 6, 16, 30, 0, 0, time.UTC)
	err := scheduling.CheckWithinWorkingHours(start, wh)
	require.Error(t, err)
	var ce *scheduling.ConstraintError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, "outside_working_hours", ce.Code)
}

func TestCheckWithinWorkingHours_WrongDay(t *testing.T) {
	t.Parallel()
	// Only Monday is configured
	wh := []scheduling.WorkingHoursSlot{
		{DayOfWeek: 0, StartHour: 9, StartMinute: 0, EndHour: 17, EndMinute: 0},
	}
	// Tuesday 10:00
	start := time.Date(2025, 1, 7, 10, 0, 0, 0, time.UTC)
	err := scheduling.CheckWithinWorkingHours(start, wh)
	require.Error(t, err)
}

// --- Cancellation credit ---

func TestCancellationEarnsCredit_Enough_Notice(t *testing.T) {
	t.Parallel()
	now := fixedTime(6, 9, 0)
	sessionStart := fixedTime(8, 9, 0) // 48h away
	require.True(t, scheduling.CancellationEarnsCredit(sessionStart, now))
}

func TestCancellationEarnsCredit_Not_Enough_Notice(t *testing.T) {
	t.Parallel()
	now := fixedTime(6, 9, 0)
	sessionStart := fixedTime(6, 20, 0) // only 11h away
	require.False(t, scheduling.CancellationEarnsCredit(sessionStart, now))
}
