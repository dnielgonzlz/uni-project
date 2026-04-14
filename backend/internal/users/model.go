package users

import (
	"time"

	"github.com/google/uuid"
)

// Role constants — used in JWT claims and DB CHECK constraints.
const (
	RoleCoach  = "coach"
	RoleClient = "client"
	RoleAdmin  = "admin"
)

// User is the base account row stored in the users table.
type User struct {
	ID           uuid.UUID  `json:"id"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"` // never serialised
	Role         string     `json:"role"`
	FullName     string     `json:"full_name"`
	PhoneE164    *string    `json:"phone,omitempty"`
	Timezone     string     `json:"timezone"`
	IsVerified   bool       `json:"is_verified"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"-"`
}

// Coach is the coach-specific profile row (1:1 with User).
type Coach struct {
	ID               uuid.UUID  `json:"id"`
	UserID           uuid.UUID  `json:"user_id"`
	BusinessName     *string    `json:"business_name,omitempty"`
	StripeAccountID  *string    `json:"-"` // internal, not exposed via API
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Client is the client-specific profile row (1:1 with User).
type Client struct {
	ID                uuid.UUID `json:"id"`
	UserID            uuid.UUID `json:"user_id"`
	CoachID           uuid.UUID `json:"coach_id"`
	TenureStartedAt   time.Time `json:"tenure_started_at"`
	SessionsPerMonth  int       `json:"sessions_per_month"`
	PriorityScore     int       `json:"priority_score"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CoachProfile is the combined view returned from the GET /coaches/{id}/profile endpoint.
type CoachProfile struct {
	User  User  `json:"user"`
	Coach Coach `json:"coach"`
}

// ClientProfile is the combined view returned from the GET /clients/{id}/profile endpoint.
type ClientProfile struct {
	User   User   `json:"user"`
	Client Client `json:"client"`
}

// UpdateCoachRequest is the body for PUT /coaches/{id}/profile.
type UpdateCoachRequest struct {
	FullName     string  `json:"full_name"     validate:"required,min=2,max=100"`
	BusinessName *string `json:"business_name" validate:"omitempty,max=120"`
	PhoneE164    *string `json:"phone"         validate:"omitempty,e164"`
	Timezone     string  `json:"timezone"      validate:"required"`
}

// UpdateClientRequest is the body for PUT /clients/{id}/profile.
type UpdateClientRequest struct {
	FullName string  `json:"full_name" validate:"required,min=2,max=100"`
	PhoneE164 *string `json:"phone"    validate:"omitempty,e164"`
	Timezone  string  `json:"timezone"  validate:"required"`
}
