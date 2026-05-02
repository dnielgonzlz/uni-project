package users

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DataExport is the full GDPR data export for a user. Returned as JSON from GET /me/export.
type DataExport struct {
	ExportedAt time.Time   `json:"exported_at"`
	User       interface{} `json:"user"`
	Profile    interface{} `json:"profile,omitempty"`
	Sessions   interface{} `json:"sessions"`
	Payments   interface{} `json:"payments"`
}

// Service contains business logic for user profile management.
type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// AssertClientPreferencesAccess checks whether the actor may read or write preferred windows
// for the given client profile ID. Coaches may read (write=false) only their own clients;
// clients may read/write only themselves; admins may always access.
func (s *Service) AssertClientPreferencesAccess(ctx context.Context, actorUserID uuid.UUID, role string, clientID uuid.UUID, write bool) error {
	client, err := s.repo.GetClientByID(ctx, clientID)
	if err != nil {
		return err
	}

	switch role {
	case RoleAdmin:
		return nil
	case RoleClient:
		if client.UserID != actorUserID {
			return ErrForbidden
		}
		return nil
	case RoleCoach:
		if write {
			// Preference slots are edited by the client (or admin); coach UI is read-only.
			return ErrForbidden
		}
		coach, err := s.repo.GetCoachByUserID(ctx, actorUserID)
		if err != nil {
			return err
		}
		if client.CoachID != coach.ID {
			return ErrForbidden
		}
		return nil
	default:
		return ErrForbidden
	}
}

// ListCoachClients returns all clients belonging to the authenticated coach user.
func (s *Service) ListCoachClients(ctx context.Context, coachUserID uuid.UUID) ([]CoachClientSummary, error) {
	coach, err := s.repo.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("users: list coach clients: %w", err)
	}

	clients, err := s.repo.ListCoachClientSummaries(ctx, coach.ID)
	if err != nil {
		return nil, fmt.Errorf("users: list coach clients: %w", err)
	}

	return clients, nil
}

// DeleteCoachClient deactivates a client owned by the authenticated coach.
func (s *Service) DeleteCoachClient(ctx context.Context, coachUserID, clientID uuid.UUID) error {
	coach, err := s.repo.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return fmt.Errorf("users: delete coach client: %w", err)
	}

	if err := s.repo.SoftDeleteClientForCoach(ctx, coach.ID, clientID); err != nil {
		return fmt.Errorf("users: delete coach client: %w", err)
	}

	return nil
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

	maxSessions := req.MaxSessionsPerDay
	if maxSessions < 2 {
		maxSessions = 4 // safe default if client omits the field
	}
	updatedCoach, err := s.repo.UpdateCoach(ctx, coachID, req.BusinessName, maxSessions)
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

// ExportUserData returns all data held for the given user ID (GDPR right of access).
func (s *Service) ExportUserData(ctx context.Context, userID uuid.UUID) (*DataExport, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("users: export — get user: %w", err)
	}

	export := &DataExport{
		ExportedAt: time.Now().UTC(),
		User: map[string]any{
			"id":         user.ID,
			"email":      user.Email,
			"full_name":  user.FullName,
			"phone":      user.PhoneE164,
			"role":       user.Role,
			"timezone":   user.Timezone,
			"created_at": user.CreatedAt,
		},
	}

	// Role-specific profile
	switch user.Role {
	case RoleCoach:
		if coach, err := s.repo.GetCoachByUserID(ctx, userID); err == nil {
			export.Profile = coach
		}
	case RoleClient:
		if client, err := s.repo.GetClientByUserID(ctx, userID); err == nil {
			export.Profile = client
		}
	}

	// Sessions and payments are fetched via raw SQL to avoid importing scheduling/billing.
	sessions, payments, err := s.repo.GetUserDataExport(ctx, userID, user.Role)
	if err != nil {
		return nil, fmt.Errorf("users: export — activity data: %w", err)
	}
	export.Sessions = sessions
	export.Payments = payments

	return export, nil
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
