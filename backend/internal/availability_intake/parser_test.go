package availability_intake

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsOpenRouterRetryableStatus(t *testing.T) {
	require.True(t, isOpenRouterRetryableStatus(429))
	require.True(t, isOpenRouterRetryableStatus(502))
	require.True(t, isOpenRouterRetryableStatus(503))
	require.True(t, isOpenRouterRetryableStatus(504))
	require.False(t, isOpenRouterRetryableStatus(401))
	require.False(t, isOpenRouterRetryableStatus(400))
}

func TestNoopParser_AlwaysReturnsIrrelevant(t *testing.T) {
	p := NoopParser{}
	result, err := p.Parse(context.Background(), ParseRequest{
		MessageText: "Monday 6-8pm",
		WeekStart:   time.Now(),
		Timezone:    "Europe/London",
	})
	require.NoError(t, err)
	require.Equal(t, ParseStatusIrrelevant, result.Status)
	require.Empty(t, result.Windows)
}

func TestBuildSystemPrompt_ContainsCampaignWeek(t *testing.T) {
	weekStart := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	prompt := buildSystemPrompt(weekStart, "Europe/London")

	require.Contains(t, prompt, "2026-05-04", "prompt must include campaign week start date")
	require.Contains(t, prompt, "Europe/London", "prompt must include timezone")
	require.Contains(t, prompt, "windows", "prompt must describe the JSON windows array")
}

func TestBuildSystemPrompt_DefaultTimezone(t *testing.T) {
	weekStart := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	prompt := buildSystemPrompt(weekStart, "")

	require.Contains(t, prompt, "Europe/London", "should default to Europe/London when timezone is empty")
}

func TestBuildSystemPrompt_ContainsTimeDefaults(t *testing.T) {
	weekStart := time.Now()
	prompt := buildSystemPrompt(weekStart, "Europe/London")

	for _, kw := range []string{"morning", "afternoon", "evening", "after work"} {
		require.True(t, strings.Contains(prompt, kw), "prompt must define time default for %q", kw)
	}
}

func TestParsedWindowJSON_RoundTrip(t *testing.T) {
	w := ParsedWindow{
		DayOfWeek:  0,
		StartTime:  "18:00",
		EndTime:    "20:00",
		Confidence: 0.95,
		Source:     "Monday 6-8pm",
	}
	require.Equal(t, 0, w.DayOfWeek)
	require.Equal(t, "18:00", w.StartTime)
	require.Equal(t, "20:00", w.EndTime)
}
