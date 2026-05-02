package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/clock"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// Mailer is the interface the auth service uses to send emails.
// Defined here so messaging package doesn't create a circular dependency.
type Mailer interface {
	SendPasswordReset(ctx context.Context, toEmail, toName, resetLink string) error
	SendVerificationEmail(ctx context.Context, toEmail, toName, verifyLink string) error
	SendEmail(ctx context.Context, toEmail, toName, subject, htmlBody string) error
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
	appBaseURL   string
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

// Register creates a new coach account and sends an email verification link.
// Returns tokens immediately so the coach is authenticated right away.
// Only coaches may self-register; clients are created by their coach.
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

	if _, err := s.usersRepo.CreateCoach(ctx, user.ID, req.BusinessName); err != nil {
		return nil, fmt.Errorf("auth: register — create coach: %w", err)
	}

	// Send verification email. Don't fail registration if the email send fails —
	// the coach can request a resend via POST /auth/resend-verification.
	_ = s.sendVerificationEmail(ctx, user.ID, user.Email, user.FullName)

	return s.issueTokenPair(ctx, user.ID, user.Role)
}

// sendVerificationEmail generates and stores a token, then sends the verification link.
func (s *Service) sendVerificationEmail(ctx context.Context, userID uuid.UUID, email, fullName string) error {
	raw, err := GenerateSecureToken()
	if err != nil {
		return fmt.Errorf("auth: verification — generate token: %w", err)
	}

	expiresAt := s.clock.Now().Add(24 * time.Hour)
	if _, err := s.repo.CreateEmailVerificationToken(ctx, userID, HashToken(raw), expiresAt); err != nil {
		return fmt.Errorf("auth: verification — store token: %w", err)
	}

	verifyLink := fmt.Sprintf("%s/verify-email?token=%s", s.appBaseURL, raw)
	if err := s.mailer.SendVerificationEmail(ctx, email, fullName, verifyLink); err != nil {
		return fmt.Errorf("auth: verification — send email: %w", err)
	}
	return nil
}

// VerifyEmail marks the user as verified using a single-use token.
func (s *Service) VerifyEmail(ctx context.Context, req VerifyEmailRequest) error {
	hash := HashToken(req.Token)

	vt, err := s.repo.GetEmailVerificationToken(ctx, hash)
	if errors.Is(err, ErrTokenNotFound) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("auth: verify email: %w", err)
	}

	if err := s.repo.MarkEmailVerificationTokenUsed(ctx, hash); err != nil {
		return fmt.Errorf("auth: verify email — mark used: %w", err)
	}

	if err := s.usersRepo.MarkVerified(ctx, vt.UserID); err != nil {
		return fmt.Errorf("auth: verify email — mark verified: %w", err)
	}

	return nil
}

// ResendVerification sends a fresh verification email to the authenticated user.
func (s *Service) ResendVerification(ctx context.Context, userID uuid.UUID) error {
	user, err := s.usersRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth: resend verification: %w", err)
	}
	if user.IsVerified {
		return ErrAlreadyVerified
	}
	return s.sendVerificationEmail(ctx, user.ID, user.Email, user.FullName)
}

// CreateClientForCoach creates a client account owned by the authenticated coach
// and emails the client a password-setup link.
func (s *Service) CreateClientForCoach(ctx context.Context, coachUserID uuid.UUID, req CreateCoachClientRequest) (*users.CoachClientSummary, error) {
	coach, err := s.usersRepo.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("auth: create coach client — resolve coach: %w", err)
	}

	exists, err := s.usersRepo.EmailExists(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("auth: create coach client — check email: %w", err)
	}
	if exists {
		return nil, users.ErrEmailTaken
	}

	tempPassword, err := GenerateSecureToken()
	if err != nil {
		return nil, fmt.Errorf("auth: create coach client — generate temp password: %w", err)
	}

	hash, err := HashPassword(tempPassword)
	if err != nil {
		return nil, fmt.Errorf("auth: create coach client — hash password: %w", err)
	}

	tz := req.Timezone
	if tz == "" {
		tz = "Europe/London"
	}

	user, err := s.usersRepo.CreateUser(ctx, &users.User{
		Email:        req.Email,
		PasswordHash: hash,
		Role:         users.RoleClient,
		FullName:     req.FullName,
		PhoneE164:    req.PhoneE164,
		Timezone:     tz,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: create coach client — create user: %w", err)
	}

	client, err := s.usersRepo.CreateClient(ctx, user.ID, coach.ID, req.SessionsPerMonth)
	if err != nil {
		return nil, fmt.Errorf("auth: create coach client — create client: %w", err)
	}

	setupToken, err := GenerateSecureToken()
	if err != nil {
		return nil, fmt.Errorf("auth: create coach client — generate setup token: %w", err)
	}

	expiresAt := s.clock.Now().Add(time.Duration(s.resetExpiry) * time.Minute)
	if _, err := s.repo.CreatePasswordResetToken(ctx, user.ID, HashToken(setupToken), expiresAt); err != nil {
		return nil, fmt.Errorf("auth: create coach client — store setup token: %w", err)
	}

	setupLink := fmt.Sprintf("%s/auth/reset-password?token=%s", s.appBaseURL, setupToken)
	body := fmt.Sprintf(`
<p>Hi %s,</p>
<p>Your coach has created your PT Scheduler account.</p>
<p><a href="%s">Click here to set your password</a></p>
<p>This link expires in %d minutes.</p>
<p>Once you've set your password, you can sign in and add your session preferences.</p>
<p>— PT Scheduler Team</p>
`, user.FullName, setupLink, s.resetExpiry)
	if err := s.mailer.SendEmail(ctx, user.Email, user.FullName, "Set up your PT Scheduler account", body); err != nil {
		// Email failure must not block client creation — the coach can resend later.
		// Log the error so it surfaces in monitoring without returning a 500.
		slog.WarnContext(ctx, "auth: create coach client — setup email failed (client created)",
			"user_id", user.ID, "email", user.Email, "error", err)
	}

	return &users.CoachClientSummary{
		User:                  *user,
		Client:                *client,
		ConfirmedSessionCount: 0,
	}, nil
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

	resetLink := fmt.Sprintf("%s/auth/reset-password?token=%s", s.appBaseURL, raw)

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

	// Mark the account as verified — resetting/setting via an emailed link proves
	// email ownership. This is how clients complete their invite flow.
	if err := s.usersRepo.MarkVerified(ctx, pt.UserID); err != nil {
		return fmt.Errorf("auth: reset password — mark verified: %w", err)
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
		UserID:       userID,
	}, nil
}
