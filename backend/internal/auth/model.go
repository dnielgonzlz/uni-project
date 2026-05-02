package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims is the payload embedded in every access token JWT.
type Claims struct {
	UserID uuid.UUID `json:"sub"`
	Role   string    `json:"role"`
	jwt.RegisteredClaims
}

// RefreshToken represents a stored refresh token (only the hash is persisted).
type RefreshToken struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"-"`
	CreatedAt time.Time  `json:"created_at"`
}

// PasswordResetToken represents a single-use password reset token.
type PasswordResetToken struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"-"`
	CreatedAt time.Time  `json:"created_at"`
}

// RegisterRequest is the body for POST /auth/register.
// Only coaches may self-register; clients are created by their coach via POST /coaches/me/clients.
type RegisterRequest struct {
	Email        string  `json:"email"         validate:"required,email"`
	Password     string  `json:"password"      validate:"required,min=8,max=72"`
	FullName     string  `json:"full_name"     validate:"required,min=2,max=100"`
	Role         string  `json:"role"          validate:"required,oneof=coach"`
	PhoneE164    *string `json:"phone"         validate:"omitempty,e164"`
	Timezone     string  `json:"timezone"      validate:"required"`
	BusinessName *string `json:"business_name" validate:"omitempty,max=120"`
}

// CreateCoachClientRequest is the body for POST /coaches/me/clients.
type CreateCoachClientRequest struct {
	Email            string  `json:"email"              validate:"required,email"`
	FullName         string  `json:"full_name"          validate:"required,min=2,max=100"`
	PhoneE164        *string `json:"phone"              validate:"omitempty,e164"`
	Timezone         string  `json:"timezone"           validate:"omitempty"`
	SessionsPerMonth int     `json:"sessions_per_month" validate:"required,min=1,max=20"`
}

// LoginRequest is the body for POST /auth/login.
type LoginRequest struct {
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// TokenResponse is returned after a successful login or refresh.
type TokenResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"` // omitted on refresh-only responses
	ExpiresIn    int       `json:"expires_in"`              // seconds
	UserID       uuid.UUID `json:"-"`                       // internal — used for audit log, never sent to client
}

// RefreshRequest is the body for POST /auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// ForgotPasswordRequest is the body for POST /auth/forgot-password.
type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// ResetPasswordRequest is the body for POST /auth/reset-password.
type ResetPasswordRequest struct {
	Token    string `json:"token"    validate:"required"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

// LogoutRequest is the body for POST /auth/logout.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// EmailVerificationToken represents a single-use email verification token.
type EmailVerificationToken struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"-"`
	CreatedAt time.Time  `json:"created_at"`
}

// VerifyEmailRequest is the body for POST /auth/verify-email.
type VerifyEmailRequest struct {
	Token string `json:"token" validate:"required"`
}
