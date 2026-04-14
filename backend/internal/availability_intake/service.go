package availability_intake

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/danielgonzalez/pt-scheduler/internal/availability"
)

// Service drives the SMS state machine for collecting client availability.
type Service struct {
	repo      *Repository
	availRepo *availability.Repository
	logger    *slog.Logger
}

func NewService(repo *Repository, availRepo *availability.Repository, logger *slog.Logger) *Service {
	return &Service{repo: repo, availRepo: availRepo, logger: logger}
}

// Handle processes an inbound SMS and returns the reply to send back via Twilio.
func (s *Service) Handle(ctx context.Context, sms InboundSMS) (reply string, err error) {
	clientID, err := s.repo.GetClientIDByPhone(ctx, sms.From)
	if err != nil {
		// Unknown sender — reply with a generic message, don't expose system info.
		return "Sorry, we couldn't find an account linked to this number. Please contact your trainer.", nil
	}

	conv, err := s.repo.GetOrCreateConversation(ctx, clientID)
	if err != nil {
		return "", fmt.Errorf("intake: get conversation: %w", err)
	}

	text := strings.TrimSpace(strings.ToLower(sms.Body))

	switch conv.State {
	case StateIdle:
		return s.handleIdle(ctx, conv, text)
	case StateAwaitingDays:
		return s.handleAwaitingDays(ctx, conv, text)
	case StateAwaitingTimes:
		return s.handleAwaitingTimes(ctx, conv, text)
	default:
		return s.handleIdle(ctx, conv, text)
	}
}

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
func (s *Service) handleAwaitingTimes(ctx context.Context, conv *Conversation, text string) (string, error) {
	startTime, endTime, ok := parseTimeRange(text)
	if !ok {
		return "Sorry, I didn't get that. Please reply with a time range like: 9am-12pm or 14:00-17:00", nil
	}

	// Retrieve stored days from conversation context
	daysRaw, _ := conv.Context["days"].([]any)
	var days []int
	for _, d := range daysRaw {
		if f, ok := d.(float64); ok {
			days = append(days, int(f))
		}
	}

	// Build preferred window entries for each day
	var entries []availability.PreferredWindowEntry
	for _, day := range days {
		entries = append(entries, availability.PreferredWindowEntry{
			DayOfWeek: day,
			StartTime: startTime,
			EndTime:   endTime,
		})
	}

	// Upsert via availability repository (SMS source)
	if err := s.saveWindowsFromSMS(ctx, conv.ClientID, entries); err != nil {
		s.logger.ErrorContext(ctx, "failed to save SMS availability", "client_id", conv.ClientID, "error", err)
		return "Something went wrong saving your availability. Please try again or contact your trainer.", nil
	}

	_, _ = s.repo.UpdateConversation(ctx, conv.ID, StateComplete, map[string]any{})

	return "Perfect! Your availability has been saved. Your trainer will be in touch with your schedule soon.", nil
}

// saveWindowsFromSMS upserts SMS-sourced preferred windows (does not overwrite manual ones).
func (s *Service) saveWindowsFromSMS(ctx context.Context, clientID interface{ String() string }, entries []availability.PreferredWindowEntry) error {
	// Reuse availability repository directly for the SMS source
	id, err := parseUUIDFromStringer(clientID)
	if err != nil {
		return err
	}
	_, err = s.availRepo.UpsertSMSWindows(ctx, id, entries)
	return err
}

// --- simple NLP helpers ---

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

// parseTimeRange tries to parse "9am-12pm", "09:00-12:00", "2pm-5pm" style ranges.
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

// normaliseTime converts "9am", "09:00", "14:00" → "09:00" style HH:MM.
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
