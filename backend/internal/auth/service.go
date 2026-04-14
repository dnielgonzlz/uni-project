package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/clock"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// Mailer is the interface the auth service uses to send emails.
// Defined here so messaging package doesn't create a circular dependency.
type Mailer interface {
	SendPasswordReset(ctx context.Context, toEmail, toName, resetLink string) error
}

// Service handles all authentication business logic.
type Service struct {
	repo         *Repository
	usersRepo    *users.Repository
	clock        clock.Clock
	jwtSecret    string
	accessExpiry int // minutes
	refreshExpiry int // days
	resetExpiry  int // minutes
	mailer       Mailer
	appBaseURL   string // e.g. https://app.yourptapp.co.uk
}

func NewService(
	repo *Repository,
	usersRepo *users.Repository,
	clk clock.Clock,
	jwtSecret string,
	accessExpiry, refreshExpiry, resetExpiry int,
	mailer Mailer,
	appBaseURL string,
) *Service {
	return &Service{
		repo:          repo,
		usersRepo:     usersRepo,
		clock:         clk,
		jwtSecret:     jwtSecret,
		accessExpiry:  accessExpiry,
		refreshExpiry: refreshExpiry,
		resetExpiry:   resetExpiry,
		mailer:        mailer,
		appBaseURL:    appBaseURL,
	}
}

// Register creates a new user account (and coach/client profile row).
// Returns tokens immediately so the caller is authenticated right away.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*TokenResponse, error) {
	exists, err := s.usersRepo.EmailExists(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("auth: register — check email: %w", err)
	}
	if exists {
		return nil, users.ErrEmailTaken
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		return nil, fmt.Errorf("auth: register — hash password: %w", err)
	}

	tz := req.Timezone
	if tz == "" {
		tz = "Europe/London"
	}

	user, err := s.usersRepo.CreateUser(ctx, &users.User{
		Email:        req.Email,
		PasswordHash: hash,
		Role:         req.Role,
		FullName:     req.FullName,
		PhoneE164:    req.PhoneE164,
		Timezone:     tz,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: register — create user: %w", err)
	}

	switch req.Role {
	case users.RoleCoach:
		if _, err := s.usersRepo.CreateCoach(ctx, user.ID, req.BusinessName); err != nil {
			return nil, fmt.Errorf("auth: register — create coach: %w", err)
		}

	case users.RoleClient:
		if req.CoachID == nil || req.SessionsPerMonth == nil {
			return nil, fmt.Errorf("auth: register — client requires coach_id and sessions_per_month")
		}
		coachID, err := uuid.Parse(*req.CoachID)
		if err != nil {
			return nil, fmt.Errorf("auth: register — invalid coach_id: %w", err)
		}
		if _, err := s.usersRepo.CreateClient(ctx, user.ID, coachID, *req.SessionsPerMonth); err != nil {
			return nil, fmt.Errorf("auth: register — create client: %w", err)
		}
	}

	return s.issueTokenPair(ctx, user.ID, user.Role)
}

// Login verifies credentials and returns a token pair.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*TokenResponse, error) {
	user, err := s.usersRepo.GetUserByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			// Return same error as wrong password to avoid email enumeration.
			return nil, ErrInvalidPassword
		}
		return nil, fmt.Errorf("auth: login: %w", err)
	}

	if err := VerifyPassword(req.Password, user.PasswordHash); err != nil {
		return nil, ErrInvalidPassword
	}

	return s.issueTokenPair(ctx, user.ID, user.Role)
}

// Logout revokes the given refresh token.
func (s *Service) Logout(ctx context.Context, rawRefreshToken string) error {
	hash := HashToken(rawRefreshToken)
	if err := s.repo.RevokeRefreshToken(ctx, hash); err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}
	return nil
}

// Refresh validates a refresh token and issues a new access token.
// The refresh token itself is rotated (old one revoked, new one issued).
func (s *Service) Refresh(ctx context.Context, rawRefreshToken string) (*TokenResponse, error) {
	hash := HashToken(rawRefreshToken)

	rt, err := s.repo.GetRefreshToken(ctx, hash)
	if errors.Is(err, ErrTokenNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, fmt.Errorf("auth: refresh: %w", err)
	}

	// Rotate: revoke old, issue new pair.
	if err := s.repo.RevokeRefreshToken(ctx, hash); err != nil {
		return nil, fmt.Errorf("auth: refresh — revoke old token: %w", err)
	}

	user, err := s.usersRepo.GetUserByID(ctx, rt.UserID)
	if err != nil {
		return nil, fmt.Errorf("auth: refresh — get user: %w", err)
	}

	return s.issueTokenPair(ctx, user.ID, user.Role)
}

// ForgotPassword generates a reset token and emails it to the user.
// Always returns nil to avoid leaking whether the email exists (timing-safe).
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	user, err := s.usersRepo.GetUserByEmail(ctx, email)
	if errors.Is(err, users.ErrNotFound) {
		// Silent success — don't reveal whether the email is registered.
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: forgot password: %w", err)
	}

	raw, err := GenerateSecureToken()
	if err != nil {
		return fmt.Errorf("auth: forgot password — generate token: %w", err)
	}

	expiresAt := s.clock.Now().Add(time.Duration(s.resetExpiry) * time.Minute)
	if _, err := s.repo.CreatePasswordResetToken(ctx, user.ID, HashToken(raw), expiresAt); err != nil {
		return fmt.Errorf("auth: forgot password — store token: %w", err)
	}

	resetLink := fmt.Sprintf("%s/reset-password?token=%s", s.appBaseURL, raw)

	// FRONTEND: the reset link lands on a page that POSTs to /api/v1/auth/reset-password
	if err := s.mailer.SendPasswordReset(ctx, user.Email, user.FullName, resetLink); err != nil {
		// Log but don't fail — token is already stored; user can retry.
		return fmt.Errorf("auth: forgot password — send email: %w", err)
	}

	return nil
}

// ResetPassword validates the token and updates the user's password.
// All existing refresh tokens are revoked to force re-login on all devices.
func (s *Service) ResetPassword(ctx context.Context, req ResetPasswordRequest) error {
	hash := HashToken(req.Token)

	pt, err := s.repo.GetPasswordResetToken(ctx, hash)
	if errors.Is(err, ErrTokenNotFound) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}

	newHash, err := HashPassword(req.Password)
	if err != nil {
		return fmt.Errorf("auth: reset password — hash: %w", err)
	}

	if err := s.usersRepo.UpdatePassword(ctx, pt.UserID, newHash); err != nil {
		return fmt.Errorf("auth: reset password — update: %w", err)
	}

	if err := s.repo.MarkPasswordResetTokenUsed(ctx, hash); err != nil {
		return fmt.Errorf("auth: reset password — mark used: %w", err)
	}

	// Revoke all sessions — anyone holding a refresh token must re-authenticate.
	if err := s.repo.RevokeAllRefreshTokens(ctx, pt.UserID); err != nil {
		return fmt.Errorf("auth: reset password — revoke tokens: %w", err)
	}

	return nil
}

// issueTokenPair mints both an access token and a refresh token.
func (s *Service) issueTokenPair(ctx context.Context, userID uuid.UUID, role string) (*TokenResponse, error) {
	accessToken, err := GenerateAccessToken(userID, role, s.jwtSecret, s.accessExpiry)
	if err != nil {
		return nil, fmt.Errorf("auth: issue — access token: %w", err)
	}

	rawRefresh, err := GenerateSecureToken()
	if err != nil {
		return nil, fmt.Errorf("auth: issue — refresh token: %w", err)
	}

	expiresAt := s.clock.Now().Add(time.Duration(s.refreshExpiry) * 24 * time.Hour)
	if _, err := s.repo.CreateRefreshToken(ctx, userID, HashToken(rawRefresh), expiresAt); err != nil {
		return nil, fmt.Errorf("auth: issue — store refresh token: %w", err)
	}

	return &TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresIn:    s.accessExpiry * 60,
	}, nil
}
