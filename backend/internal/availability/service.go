package availability

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Service handles business logic for availability management.
type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetWorkingHours(ctx context.Context, coachID uuid.UUID) ([]WorkingHours, error) {
	hours, err := s.repo.GetWorkingHours(ctx, coachID)
	if err != nil {
		return nil, fmt.Errorf("availability: get working hours: %w", err)
	}
	return hours, nil
}

func (s *Service) SetWorkingHours(ctx context.Context, coachID uuid.UUID, req SetWorkingHoursRequest) ([]WorkingHours, error) {
	hours, err := s.repo.ReplaceWorkingHours(ctx, coachID, req.Hours)
	if err != nil {
		return nil, fmt.Errorf("availability: set working hours: %w", err)
	}
	return hours, nil
}

func (s *Service) GetPreferredWindows(ctx context.Context, clientID uuid.UUID) ([]PreferredWindow, error) {
	windows, err := s.repo.GetPreferredWindows(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("availability: get preferred windows: %w", err)
	}
	return windows, nil
}

func (s *Service) SetPreferredWindows(ctx context.Context, clientID uuid.UUID, req SetPreferencesRequest) ([]PreferredWindow, error) {
	windows, err := s.repo.ReplacePreferredWindows(ctx, clientID, req.Windows)
	if err != nil {
		return nil, fmt.Errorf("availability: set preferred windows: %w", err)
	}
	return windows, nil
}
