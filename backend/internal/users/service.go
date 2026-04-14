package users

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Service contains business logic for user profile management.
type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// GetCoachProfile returns the combined user + coach profile.
func (s *Service) GetCoachProfile(ctx context.Context, coachID uuid.UUID) (*CoachProfile, error) {
	coach, err := s.repo.GetCoachByID(ctx, coachID)
	if err != nil {
		return nil, fmt.Errorf("users: get coach profile: %w", err)
	}

	user, err := s.repo.GetUserByID(ctx, coach.UserID)
	if err != nil {
		return nil, fmt.Errorf("users: get coach user: %w", err)
	}

	return &CoachProfile{User: *user, Coach: *coach}, nil
}

// UpdateCoachProfile updates the user name/phone/timezone and the coach business name.
func (s *Service) UpdateCoachProfile(ctx context.Context, coachID uuid.UUID, req UpdateCoachRequest) (*CoachProfile, error) {
	coach, err := s.repo.GetCoachByID(ctx, coachID)
	if err != nil {
		return nil, fmt.Errorf("users: update coach — fetch coach: %w", err)
	}

	user, err := s.repo.UpdateUser(ctx, coach.UserID, req.FullName, req.PhoneE164, req.Timezone)
	if err != nil {
		return nil, fmt.Errorf("users: update coach — update user: %w", err)
	}

	updatedCoach, err := s.repo.UpdateCoach(ctx, coachID, req.BusinessName)
	if err != nil {
		return nil, fmt.Errorf("users: update coach — update coach: %w", err)
	}

	return &CoachProfile{User: *user, Coach: *updatedCoach}, nil
}

// GetClientProfile returns the combined user + client profile.
func (s *Service) GetClientProfile(ctx context.Context, clientID uuid.UUID) (*ClientProfile, error) {
	client, err := s.repo.GetClientByID(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("users: get client profile: %w", err)
	}

	user, err := s.repo.GetUserByID(ctx, client.UserID)
	if err != nil {
		return nil, fmt.Errorf("users: get client user: %w", err)
	}

	return &ClientProfile{User: *user, Client: *client}, nil
}

// UpdateClientProfile updates the user name/phone/timezone for a client.
func (s *Service) UpdateClientProfile(ctx context.Context, clientID uuid.UUID, req UpdateClientRequest) (*ClientProfile, error) {
	client, err := s.repo.GetClientByID(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("users: update client — fetch client: %w", err)
	}

	user, err := s.repo.UpdateUser(ctx, client.UserID, req.FullName, req.PhoneE164, req.Timezone)
	if err != nil {
		return nil, fmt.Errorf("users: update client — update user: %w", err)
	}

	return &ClientProfile{User: *user, Client: *client}, nil
}
