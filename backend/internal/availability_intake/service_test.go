package availability_intake

// Internal package tests — uses unexported types and fakes to keep the
// AvailabilityParser interface and handleWithAI logic fully testable without a DB.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/danielgonzalez/pt-scheduler/internal/availability"
)

// ─── Fakes ────────────────────────────────────────────────────────────────────

type fakeParser struct {
	result *ParseResult
	err    error
	calls  int
	// results lets tests specify different results per call (first, second, …)
	results []*ParseResult
}

func (f *fakeParser) Parse(_ context.Context, _ ParseRequest) (*ParseResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.results) > 0 && f.calls < len(f.results) {
		r := f.results[f.calls]
		f.calls++
		return r, nil
	}
	f.calls++
	return f.result, nil
}

// fakeRepo is an in-memory implementation of repoStore.
type fakeRepo struct {
	client        *InboundClient
	clientErr     error
	conversation  *Conversation
	weekStart     time.Time
	weekStartErr  error
	lastState     string
	lastCtx       map[string]any
	parseStatusSaved string
}

func (f *fakeRepo) GetClientByPhone(_ context.Context, _ string) (*InboundClient, error) {
	return f.client, f.clientErr
}

func (f *fakeRepo) RecordWebhookEvent(_ context.Context, _ string, _ map[string][]string) error {
	return nil
}

func (f *fakeRepo) MarkLatestRecipientReplied(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

func (f *fakeRepo) GetOrCreateConversation(_ context.Context, _ uuid.UUID) (*Conversation, error) {
	if f.conversation == nil {
		f.conversation = &Conversation{
			ID:       uuid.New(),
			ClientID: uuid.New(),
			State:    StateIdle,
			Context:  map[string]any{},
		}
	}
	return f.conversation, nil
}

func (f *fakeRepo) UpdateConversation(_ context.Context, _ uuid.UUID, state string, ctx2 map[string]any) (*Conversation, error) {
	f.lastState = state
	f.lastCtx = ctx2
	f.conversation.State = state
	f.conversation.Context = ctx2
	return f.conversation, nil
}

func (f *fakeRepo) GetLatestCampaignWeekStart(_ context.Context, _ uuid.UUID) (time.Time, error) {
	return f.weekStart, f.weekStartErr
}

func (f *fakeRepo) UpdateRecipientParseResult(_ context.Context, _ uuid.UUID, parseStatus string, _ []byte) error {
	f.parseStatusSaved = parseStatus
	return nil
}

// fakeAvailRepo is an in-memory implementation of availStore.
type fakeAvailRepo struct {
	saved []availability.PreferredWindowEntry
}

func (f *fakeAvailRepo) UpsertTwilioWindows(_ context.Context, _ interface{ String() string }, _ string, entries []availability.PreferredWindowEntry) ([]availability.PreferredWindow, error) {
	f.saved = append(f.saved, entries...)
	return nil, nil
}

// newTestService creates a Service wired with the given fakes.
func newTestService(repo *fakeRepo, avail *fakeAvailRepo, parser AvailabilityParser) *Service {
	return &Service{
		repo:      repo,
		availRepo: avail,
		parser:    parser,
		logger:    noopLogger(),
	}
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newMsg(body string) InboundMessage {
	return InboundMessage{
		MessageSID: uuid.NewString(),
		From:       "+447700900001",
		Body:       body,
		Channel:    ChannelWhatsApp,
	}
}

func allowlistedClient() *InboundClient {
	return &InboundClient{ID: uuid.New(), AIBookingEnabled: true}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestHandle_UnknownSender_ReturnsRejection(t *testing.T) {
	repo := &fakeRepo{clientErr: errors.New("not found")}
	svc := newTestService(repo, &fakeAvailRepo{}, NoopParser{})

	reply, err := svc.Handle(context.Background(), newMsg("hello"))

	require.NoError(t, err)
	require.Contains(t, reply, "don't recognise")
}

func TestHandle_NotAllowlisted_ReturnsRejection(t *testing.T) {
	repo := &fakeRepo{client: &InboundClient{ID: uuid.New(), AIBookingEnabled: false}}
	svc := newTestService(repo, &fakeAvailRepo{}, NoopParser{})

	reply, err := svc.Handle(context.Background(), newMsg("Monday 6pm"))

	require.NoError(t, err)
	require.Contains(t, reply, "not enabled")
}

func TestHandle_AI_ParsedMessage_SavesWindowsAndCompletes(t *testing.T) {
	repo := &fakeRepo{client: allowlistedClient()}
	avail := &fakeAvailRepo{}
	parser := &fakeParser{result: &ParseResult{
		Status: ParseStatusParsed,
		Windows: []ParsedWindow{
			{DayOfWeek: 0, StartTime: "18:00", EndTime: "20:00", Confidence: 0.95, Source: "Monday 6-8pm"},
			{DayOfWeek: 2, StartTime: "17:00", EndTime: "21:00", Confidence: 0.9, Source: "Wednesday after 5pm"},
		},
	}}
	svc := newTestService(repo, avail, parser)

	reply, err := svc.Handle(context.Background(), newMsg("I can do Monday 6-8pm, Wednesday after 5pm"))

	require.NoError(t, err)
	require.Contains(t, reply, "saved your availability")
	require.Equal(t, StateComplete, repo.lastState)
	require.Len(t, avail.saved, 2)
	require.Equal(t, "parsed", repo.parseStatusSaved)
}

func TestHandle_AI_AmbiguousMessage_SendsFollowUp(t *testing.T) {
	repo := &fakeRepo{client: allowlistedClient()}
	parser := &fakeParser{result: &ParseResult{
		Status:   ParseStatusAmbiguous,
		FollowUp: "Which days work for you next week?",
	}}
	svc := newTestService(repo, &fakeAvailRepo{}, parser)

	reply, err := svc.Handle(context.Background(), newMsg("evenings work for me"))

	require.NoError(t, err)
	require.Equal(t, "Which days work for you next week?", reply)
	require.Equal(t, StateAwaitingClarification, repo.lastState)
}

func TestHandle_AI_ClarificationResolves_SavesAndCompletes(t *testing.T) {
	repo := &fakeRepo{
		client: allowlistedClient(),
		conversation: &Conversation{
			ID:       uuid.New(),
			ClientID: uuid.New(),
			State:    StateAwaitingClarification,
			Context: map[string]any{
				"original_message": "evenings work",
				"parse_attempts":   float64(1),
			},
		},
	}
	avail := &fakeAvailRepo{}
	parser := &fakeParser{result: &ParseResult{
		Status: ParseStatusParsed,
		Windows: []ParsedWindow{
			{DayOfWeek: 1, StartTime: "17:00", EndTime: "21:00", Confidence: 0.9},
		},
	}}
	svc := newTestService(repo, avail, parser)

	reply, err := svc.Handle(context.Background(), newMsg("Tuesday and Thursday"))

	require.NoError(t, err)
	require.Contains(t, reply, "saved your availability")
	require.Equal(t, StateComplete, repo.lastState)
	require.NotEmpty(t, avail.saved)
}

func TestHandle_AI_ThreeAmbiguousAttempts_GivesUpGracefully(t *testing.T) {
	repo := &fakeRepo{
		client: allowlistedClient(),
		conversation: &Conversation{
			ID:      uuid.New(),
			State:   StateAwaitingClarification,
			Context: map[string]any{"parse_attempts": float64(2)},
		},
	}
	parser := &fakeParser{result: &ParseResult{
		Status:   ParseStatusAmbiguous,
		FollowUp: "Which days?",
	}}
	svc := newTestService(repo, &fakeAvailRepo{}, parser)

	reply, err := svc.Handle(context.Background(), newMsg("not sure"))

	require.NoError(t, err)
	require.Contains(t, reply, "trainer will get in touch")
	require.Equal(t, StateComplete, repo.lastState)
}

func TestHandle_AI_IrrelevantMessage_NoReply(t *testing.T) {
	repo := &fakeRepo{client: allowlistedClient()}
	parser := &fakeParser{result: &ParseResult{Status: ParseStatusIrrelevant}}
	svc := newTestService(repo, &fakeAvailRepo{}, parser)

	reply, err := svc.Handle(context.Background(), newMsg("ok thanks"))

	require.NoError(t, err)
	require.Empty(t, reply)
}

func TestHandle_AI_ParserError_FallsBackToRuleBased(t *testing.T) {
	repo := &fakeRepo{client: allowlistedClient()}
	parser := &fakeParser{err: errors.New("openrouter timeout")}
	svc := newTestService(repo, &fakeAvailRepo{}, parser)

	// Rule-based flow starts with a day-prompt when in idle state
	reply, err := svc.Handle(context.Background(), newMsg("Monday"))

	require.NoError(t, err)
	require.True(t, strings.Contains(reply, "days") || strings.Contains(reply, "Which"), "expected rule-based prompt, got: %q", reply)
}

// ─── Pure helper tests ────────────────────────────────────────────────────────

func TestNextMonday_FromWednesday(t *testing.T) {
	// Wednesday 2026-04-29 → next Monday should be 2026-05-04
	wed := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	got := nextMonday(wed)
	require.Equal(t, 2026, got.Year())
	require.Equal(t, time.May, got.Month())
	require.Equal(t, 4, got.Day())
	require.Equal(t, time.Monday, got.Weekday())
}

func TestNextMonday_FromMonday_AdvancesOneWeek(t *testing.T) {
	// Monday 2026-04-27 → next Monday is 2026-05-04
	mon := time.Date(2026, 4, 27, 9, 0, 0, 0, time.UTC)
	got := nextMonday(mon)
	require.Equal(t, time.Monday, got.Weekday())
	require.Equal(t, 4, got.Day())
}
