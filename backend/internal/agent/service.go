package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/danielgonzalez/pt-scheduler/internal/users"
	"github.com/google/uuid"
)

type coachLookup interface {
	GetCoachByUserID(ctx context.Context, userID uuid.UUID) (*users.Coach, error)
}

var ErrAgentNotReady = errors.New("agent is not ready to send campaigns")

type TemplateMessenger interface {
	SendContentTemplate(ctx context.Context, toE164, contentSID string, variables map[string]string) (string, error)
}

type Service struct {
	repo    *Repository
	users   coachLookup
	checker ApprovalChecker
	msg     TemplateMessenger
	logger  *slog.Logger
}

func NewService(repo *Repository, users coachLookup, checker ApprovalChecker, msg TemplateMessenger, logger *slog.Logger) *Service {
	return &Service{repo: repo, users: users, checker: checker, msg: msg, logger: logger}
}

func (s *Service) GetSettings(ctx context.Context, coachUserID uuid.UUID) (*Settings, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("agent: get coach for settings: %w", err)
	}
	return s.repo.GetOrCreateSettings(ctx, coach.ID)
}

func (s *Service) UpdateSettings(ctx context.Context, coachUserID uuid.UUID, req UpdateSettingsRequest) (*Settings, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("agent: get coach for update settings: %w", err)
	}
	return s.repo.UpdateSettings(ctx, coach.ID, req)
}

func (s *Service) CheckTemplateStatus(ctx context.Context, coachUserID uuid.UUID) (*TemplateStatusResponse, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("agent: get coach for template check: %w", err)
	}

	settings, err := s.repo.GetOrCreateSettings(ctx, coach.ID)
	if err != nil {
		return nil, err
	}
	if settings.TemplateSID == nil || *settings.TemplateSID == "" {
		return &TemplateStatusResponse{TemplateStatus: TemplateStatusMissing}, nil
	}

	result, err := s.checker.CheckTemplateStatus(ctx, *settings.TemplateSID)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.UpdateTemplateStatus(ctx, coach.ID, result.TemplateStatus); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ListClients(ctx context.Context, coachUserID uuid.UUID) ([]AgentClient, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("agent: get coach for clients: %w", err)
	}
	return s.repo.ListClients(ctx, coach.ID)
}

func (s *Service) UpdateClientEnabled(ctx context.Context, coachUserID, clientID uuid.UUID, enabled bool) (*AgentClient, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("agent: get coach for update client: %w", err)
	}
	return s.repo.UpdateClientEnabled(ctx, coach.ID, clientID, enabled)
}

func (s *Service) GetOverview(ctx context.Context, coachUserID uuid.UUID) (*CampaignOverview, error) {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("agent: get coach for overview: %w", err)
	}
	return s.repo.GetOverview(ctx, coach.ID)
}

func (s *Service) RunDueCampaigns(ctx context.Context, now time.Time) error {
	if s.msg == nil {
		return nil
	}

	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		loc = time.UTC
	}

	weekStart, sendAt, due := fridayPromptWindow(now, loc)
	if !due {
		return nil
	}

	coaches, err := s.repo.ListCampaignCoaches(ctx)
	if err != nil {
		return err
	}

	for _, coach := range coaches {
		if err := s.sendCampaignForCoach(ctx, coach, weekStart, sendAt); err != nil && s.logger != nil {
			s.logger.WarnContext(ctx, "agent campaign failed", "coach_id", coach.CoachID, "error", err)
		}
	}
	return nil
}

func (s *Service) SendCampaignNow(ctx context.Context, coachUserID uuid.UUID, now time.Time) error {
	coach, err := s.users.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return fmt.Errorf("agent: get coach for send now: %w", err)
	}

	settings, err := s.repo.GetOrCreateSettings(ctx, coach.ID)
	if err != nil {
		return err
	}
	if !settings.Enabled || settings.TemplateSID == nil || settings.TemplateStatus != TemplateStatusApproved {
		return ErrAgentNotReady
	}

	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		loc = time.UTC
	}

	weekStart := nextWeekStart(now, loc)
	return s.sendCampaignForCoach(ctx, CampaignCoach{
		CoachID:     coach.ID,
		TemplateSID: *settings.TemplateSID,
	}, weekStart, now)
}

func (s *Service) sendCampaignForCoach(ctx context.Context, coach CampaignCoach, weekStart, sendAt time.Time) error {
	campaignID, created, err := s.repo.CreateCampaign(ctx, coach.CoachID, weekStart, sendAt)
	if err != nil {
		return err
	}
	_ = created

	clients, err := s.repo.ListCampaignClients(ctx, coach.CoachID)
	if err != nil {
		return err
	}
	if len(clients) == 0 && s.logger != nil {
		s.logger.WarnContext(ctx, "agent campaign has no eligible clients", "coach_id", coach.CoachID)
	}

	for _, client := range clients {
		recipient, err := s.repo.GetOrCreateCampaignRecipient(ctx, campaignID, client.ClientID)
		if err != nil {
			return err
		}

		if recipient.Status == "sent" || recipient.Status == "replied" {
			if s.logger != nil {
				s.logger.InfoContext(ctx, "agent campaign recipient already contacted", "coach_id", coach.CoachID, "client_id", client.ClientID, "status", recipient.Status)
			}
			continue
		}

		messageSID, err := s.msg.SendContentTemplate(ctx, client.Phone, coach.TemplateSID, map[string]string{
			"1": firstName(client.FullName),
		})
		if err != nil {
			if s.logger != nil {
				s.logger.WarnContext(ctx, "agent campaign template send failed", "coach_id", coach.CoachID, "client_id", client.ClientID, "error", err)
			}
			_ = s.repo.MarkRecipientFailed(ctx, recipient.ID)
			continue
		}
		if err := s.repo.MarkRecipientSent(ctx, recipient.ID, messageSID); err != nil {
			return err
		}
	}

	return s.repo.MarkCampaignSent(ctx, campaignID)
}

func fridayPromptWindow(now time.Time, loc *time.Location) (weekStart time.Time, sendAt time.Time, due bool) {
	localNow := now.In(loc)
	weekdayOffset := (int(localNow.Weekday()) - int(time.Monday) + 7) % 7
	monday := dateOnly(localNow.AddDate(0, 0, -weekdayOffset), loc)
	friday := monday.AddDate(0, 0, 4)
	sendAt = time.Date(friday.Year(), friday.Month(), friday.Day(), 18, 0, 0, 0, loc)
	weekStart = monday.AddDate(0, 0, 7)

	return weekStart, sendAt, !localNow.Before(sendAt) && localNow.Before(weekStart)
}

func dateOnly(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

func nextWeekStart(now time.Time, loc *time.Location) time.Time {
	localNow := now.In(loc)
	weekdayOffset := (int(localNow.Weekday()) - int(time.Monday) + 7) % 7
	currentMonday := dateOnly(localNow.AddDate(0, 0, -weekdayOffset), loc)
	return currentMonday.AddDate(0, 0, 7)
}

func firstName(fullName string) string {
	parts := strings.Fields(fullName)
	if len(parts) == 0 {
		return "there"
	}
	return parts[0]
}
