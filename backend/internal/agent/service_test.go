package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFridayPromptWindow(t *testing.T) {
	loc := mustLondon(t)

	t.Run("not due before friday 18", func(t *testing.T) {
		now := time.Date(2026, 5, 1, 17, 59, 0, 0, loc) // Friday
		weekStart, sendAt, due := fridayPromptWindow(now, loc)

		require.False(t, due)
		require.Equal(t, time.Date(2026, 5, 4, 0, 0, 0, 0, loc), weekStart)
		require.Equal(t, time.Date(2026, 5, 1, 18, 0, 0, 0, loc), sendAt)
	})

	t.Run("due from friday 18 until next monday", func(t *testing.T) {
		now := time.Date(2026, 5, 1, 18, 0, 0, 0, loc) // Friday
		weekStart, sendAt, due := fridayPromptWindow(now, loc)

		require.True(t, due)
		require.Equal(t, time.Date(2026, 5, 4, 0, 0, 0, 0, loc), weekStart)
		require.Equal(t, time.Date(2026, 5, 1, 18, 0, 0, 0, loc), sendAt)
	})

	t.Run("not due once target week starts", func(t *testing.T) {
		now := time.Date(2026, 5, 4, 9, 0, 0, 0, loc) // Monday
		_, _, due := fridayPromptWindow(now, loc)

		require.False(t, due)
	})
}

func TestNormaliseTemplateStatus(t *testing.T) {
	require.Equal(t, TemplateStatusApproved, normaliseTemplateStatus("approved"))
	require.Equal(t, TemplateStatusRejected, normaliseTemplateStatus("rejected"))
	require.Equal(t, TemplateStatusPending, normaliseTemplateStatus("received"))
	require.Equal(t, TemplateStatusPending, normaliseTemplateStatus("unknown"))
}

func TestFirstName(t *testing.T) {
	require.Equal(t, "Daniel", firstName("Daniel Gonzalez"))
	require.Equal(t, "there", firstName("   "))
}

func TestNextWeekStart(t *testing.T) {
	loc := mustLondon(t)
	now := time.Date(2026, 4, 28, 16, 0, 0, 0, loc) // Tuesday

	require.Equal(t, time.Date(2026, 5, 4, 0, 0, 0, 0, loc), nextWeekStart(now, loc))
}

func mustLondon(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	require.NoError(t, err)
	return loc
}
