package availability_intake

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/availability"
)

// repoStore is the subset of Repository methods the service needs.
// RecordWebhookEvent is invoked from Handler.InboundSMS only (idempotency); Service.Handle must not call it.
type repoStore interface {
	GetClientByPhone(ctx context.Context, phoneE164 string) (*InboundClient, error)
	RecordWebhookEvent(ctx context.Context, messageSID string, payload map[string][]string) error
	MarkLatestRecipientReplied(ctx context.Context, clientID uuid.UUID, rawReply string) error
	GetOrCreateConversation(ctx context.Context, clientID uuid.UUID) (*Conversation, error)
	UpdateConversation(ctx context.Context, id uuid.UUID, state string, ctx2 map[string]any) (*Conversation, error)
	GetLatestCampaignWeekStart(ctx context.Context, clientID uuid.UUID) (time.Time, error)
	UpdateRecipientParseResult(ctx context.Context, clientID uuid.UUID, parseStatus string, parsedWindowsJSON []byte) error
}

// availStore is the subset of availability.Repository methods the service needs.
type availStore interface {
	UpsertTwilioWindows(ctx context.Context, clientID interface{ String() string }, source string, entries []availability.PreferredWindowEntry) ([]availability.PreferredWindow, error)
}

// Service drives the Twilio state machine for collecting client availability.
type Service struct {
	repo      repoStore
	availRepo availStore
	parser    AvailabilityParser // nil or NoopParser → rule-based fallback
	logger    *slog.Logger
}

func NewService(repo *Repository, availRepo *availability.Repository, parser AvailabilityParser, logger *slog.Logger) *Service {
	return &Service{repo: repo, availRepo: availRepo, parser: parser, logger: logger}
}

// Handle processes an inbound Twilio message and returns the reply to send back.
func (s *Service) Handle(ctx context.Context, msg InboundMessage) (reply string, err error) {
	client, err := s.repo.GetClientByPhone(ctx, msg.From)
	if err != nil {
		return "Hey! I don't recognise this number as one of my clients yet. Please contact your coach directly.", nil
	}
	if !client.AIBookingEnabled {
		return "Your coach has not enabled WhatsApp scheduling for you yet. Please contact them directly.", nil
	}

	// Webhook idempotency is enforced in Handler.InboundSMS via RecordWebhookEvent (full form payload).
	// Do not insert the same MessageSid again here or the second insert conflicts and the message is skipped.

	if err := s.repo.MarkLatestRecipientReplied(ctx, client.ID, msg.Body); err != nil {
		s.logger.WarnContext(ctx, "failed to mark campaign reply", "client_id", client.ID, "error", err)
	}

	conv, err := s.repo.GetOrCreateConversation(ctx, client.ID)
	if err != nil {
		return "", fmt.Errorf("intake: get conversation: %w", err)
	}

	// AI path — active whenever a real parser is wired in.
	if _, isNoop := s.parser.(NoopParser); s.parser != nil && !isNoop {
		return s.handleWithAI(ctx, client, conv, msg)
	}

	// Rule-based fallback (kept as-is for when OPENROUTER_API_KEY is absent).
	text := strings.TrimSpace(strings.ToLower(msg.Body))
	switch conv.State {
	case StateIdle, StateComplete, StateAwaitingClarification:
		return s.handleIdle(ctx, conv, text)
	case StateAwaitingDays:
		return s.handleAwaitingDays(ctx, conv, text)
	case StateAwaitingTimes:
		return s.handleAwaitingTimes(ctx, conv, text, msg.Channel)
	default:
		return s.handleIdle(ctx, conv, text)
	}
}

// handleWithAI uses the LLM parser to extract availability from free-form text.
func (s *Service) handleWithAI(ctx context.Context, client *InboundClient, conv *Conversation, msg InboundMessage) (string, error) {
	// Determine the campaign week for the prompt so the LLM resolves day names correctly.
	weekStart, err := s.repo.GetLatestCampaignWeekStart(ctx, client.ID)
	if err != nil {
		// No active campaign found — use next Monday as a safe default.
		weekStart = nextMonday(time.Now())
	}

	// If we're waiting for a clarification reply, prepend the original message so the
	// LLM has full context to resolve the ambiguity.
	messageText := msg.Body
	if conv.State == StateAwaitingClarification {
		if orig, ok := conv.Context["original_message"].(string); ok && orig != "" {
			messageText = orig + "\n" + msg.Body
		}
	}

	result, err := s.parser.Parse(ctx, ParseRequest{
		MessageText: messageText,
		WeekStart:   weekStart,
		Timezone:    "Europe/London",
	})
	if err != nil {
		s.logger.WarnContext(ctx, "AI parser failed; falling back to rule-based intake", "error", err)
		text := strings.TrimSpace(strings.ToLower(msg.Body))
		return s.handleIdle(ctx, conv, text)
	}

	switch result.Status {
	case ParseStatusParsed:
		return s.commitParsedWindows(ctx, client, conv, msg.Channel, result)

	case ParseStatusAmbiguous:
		attempts := 0
		if a, ok := conv.Context["parse_attempts"].(float64); ok {
			attempts = int(a)
		}
		if attempts >= 2 {
			_, _ = s.repo.UpdateConversation(ctx, conv.ID, StateComplete, map[string]any{})
			return "No worries — your trainer will get in touch to sort your schedule.", nil
		}
		originalMsg := msg.Body
		if conv.State == StateAwaitingClarification {
			if orig, ok := conv.Context["original_message"].(string); ok && orig != "" {
				originalMsg = orig
			}
		}
		_, _ = s.repo.UpdateConversation(ctx, conv.ID, StateAwaitingClarification, map[string]any{
			"original_message": originalMsg,
			"parse_attempts":   float64(attempts + 1),
		})
		followUp := result.FollowUp
		if followUp == "" {
			followUp = "Could you list which days and times work for you next week?"
		}
		return followUp, nil

	case ParseStatusIrrelevant:
		return "", nil

	default:
		return "", nil
	}
}

// commitParsedWindows saves the AI-extracted windows and marks the conversation complete.
func (s *Service) commitParsedWindows(ctx context.Context, client *InboundClient, conv *Conversation, channel string, result *ParseResult) (string, error) {
	entries := make([]availability.PreferredWindowEntry, 0, len(result.Windows))
	for _, w := range result.Windows {
		entries = append(entries, availability.PreferredWindowEntry{
			DayOfWeek: w.DayOfWeek,
			StartTime: w.StartTime,
			EndTime:   w.EndTime,
		})
	}

	source := availabilitySourceForChannel(channel)
	if err := s.saveWindowsFromTwilio(ctx, conv.ClientID, source, entries); err != nil {
		s.logger.ErrorContext(ctx, "failed to save AI-parsed availability",
			"client_id", conv.ClientID, "error", err)
		return "Something went wrong saving your availability. Please try again or contact your trainer.", nil
	}

	parsedJSON, _ := json.Marshal(result.Windows)
	if err := s.repo.UpdateRecipientParseResult(ctx, client.ID, "parsed", parsedJSON); err != nil {
		s.logger.WarnContext(ctx, "failed to update recipient parse result", "client_id", client.ID, "error", err)
	}

	_, _ = s.repo.UpdateConversation(ctx, conv.ID, StateComplete, map[string]any{})

	return "Thanks! I've saved your availability for next week. Your trainer will be in touch with your schedule soon.", nil
}

// nextMonday returns the date of the coming Monday (or today if today is Monday).
func nextMonday(now time.Time) time.Time {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		loc = time.UTC
	}
	now = now.In(loc)
	dow := int(now.Weekday()) // Sunday=0
	if dow == 0 {
		dow = 7 // treat Sunday as 7 so Monday=1 is always ≥ 1
	}
	daysUntilMonday := (8 - dow) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	next := now.AddDate(0, 0, daysUntilMonday)
	return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc)
}

// ─── Rule-based fallback handlers (unchanged) ────────────────────────────────

// handleIdle starts a new intake session when the client replies to the coach's prompt.
func (s *Service) handleIdle(ctx context.Context, conv *Conversation, text string) (string, error) {
	_, err := s.repo.UpdateConversation(ctx, conv.ID, StateAwaitingDays, map[string]any{})
	if err != nil {
		return "", err
	}
	return "Hi! Which days are you available next week? " +
		"Reply with day names, e.g.: Monday, Wednesday, Friday", nil
}

// handleAwaitingDays parses day names from the reply and moves to the times step.
func (s *Service) handleAwaitingDays(ctx context.Context, conv *Conversation, text string) (string, error) {
	days := parseDays(text)
	if len(days) == 0 {
		return "Sorry, I didn't catch that. Please reply with day names, e.g.: Monday, Wednesday", nil
	}

	newCtx := map[string]any{"days": days}
	_, err := s.repo.UpdateConversation(ctx, conv.ID, StateAwaitingTimes, newCtx)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Got it — %s. What times work best? Reply like: 9am-12pm or 2pm-5pm", strings.Join(intsToDayNames(days), ", ")), nil
}

// handleAwaitingTimes parses a time range and saves preferred windows to the DB.
func (s *Service) handleAwaitingTimes(ctx context.Context, conv *Conversation, text, channel string) (string, error) {
	startTime, endTime, ok := parseTimeRange(text)
	if !ok {
		return "Sorry, I didn't get that. Please reply with a time range like: 9am-12pm or 14:00-17:00", nil
	}

	daysRaw, _ := conv.Context["days"].([]any)
	var days []int
	for _, d := range daysRaw {
		if f, ok := d.(float64); ok {
			days = append(days, int(f))
		}
	}

	var entries []availability.PreferredWindowEntry
	for _, day := range days {
		entries = append(entries, availability.PreferredWindowEntry{
			DayOfWeek: day,
			StartTime: startTime,
			EndTime:   endTime,
		})
	}

	source := availabilitySourceForChannel(channel)
	if err := s.saveWindowsFromTwilio(ctx, conv.ClientID, source, entries); err != nil {
		s.logger.ErrorContext(ctx, "failed to save Twilio availability", "client_id", conv.ClientID, "source", source, "error", err)
		return "Something went wrong saving your availability. Please try again or contact your trainer.", nil
	}

	_, _ = s.repo.UpdateConversation(ctx, conv.ID, StateComplete, map[string]any{})

	return "Perfect! Your availability has been saved. Your trainer will be in touch with your schedule soon.", nil
}

// saveWindowsFromTwilio upserts Twilio-sourced preferred windows without overwriting manual ones.
func (s *Service) saveWindowsFromTwilio(ctx context.Context, clientID interface{ String() string }, source string, entries []availability.PreferredWindowEntry) error {
	id, err := parseUUIDFromStringer(clientID)
	if err != nil {
		return err
	}
	_, err = s.availRepo.UpsertTwilioWindows(ctx, id, source, entries)
	return err
}

func availabilitySourceForChannel(channel string) string {
	if channel == ChannelWhatsApp {
		return ChannelWhatsApp
	}
	return ChannelSMS
}

// ─── Simple NLP helpers (rule-based fallback) ─────────────────────────────────

var dayNames = map[string]int{
	"monday": 0, "mon": 0,
	"tuesday": 1, "tue": 1, "tues": 1,
	"wednesday": 2, "wed": 2,
	"thursday": 3, "thu": 3, "thur": 3, "thurs": 3,
	"friday": 4, "fri": 4,
	"saturday": 5, "sat": 5,
	"sunday": 6, "sun": 6,
}

func parseDays(text string) []int {
	seen := map[int]bool{}
	var days []int
	for word, dow := range dayNames {
		if strings.Contains(text, word) && !seen[dow] {
			seen[dow] = true
			days = append(days, dow)
		}
	}
	return days
}

var dowLabels = []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}

func intsToDayNames(days []int) []string {
	names := make([]string, 0, len(days))
	for _, d := range days {
		if d >= 0 && d <= 6 {
			names = append(names, dowLabels[d])
		}
	}
	return names
}

func parseTimeRange(text string) (start, end string, ok bool) {
	separators := []string{"-", "to", "–", "—"}
	for _, sep := range separators {
		idx := strings.Index(text, sep)
		if idx > 0 {
			rawStart := strings.TrimSpace(text[:idx])
			rawEnd := strings.TrimSpace(text[idx+len(sep):])
			s, okS := normaliseTime(rawStart)
			e, okE := normaliseTime(rawEnd)
			if okS && okE {
				return s, e, true
			}
		}
	}
	return "", "", false
}

func normaliseTime(raw string) (string, bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	isPM := strings.HasSuffix(raw, "pm")
	isAM := strings.HasSuffix(raw, "am")

	raw = strings.TrimSuffix(raw, "pm")
	raw = strings.TrimSuffix(raw, "am")
	raw = strings.TrimSpace(raw)

	var hour, minute int
	if strings.Contains(raw, ":") {
		if _, err := fmt.Sscanf(raw, "%d:%d", &hour, &minute); err != nil {
			return "", false
		}
	} else {
		if _, err := fmt.Sscanf(raw, "%d", &hour); err != nil {
			return "", false
		}
	}

	if isPM && hour < 12 {
		hour += 12
	}
	if isAM && hour == 12 {
		hour = 0
	}

	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return "", false
	}

	return fmt.Sprintf("%02d:%02d", hour, minute), true
}

func parseUUIDFromStringer(v interface{ String() string }) (interface{ String() string }, error) {
	return v, nil
}
