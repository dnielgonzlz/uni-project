package users

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ErrEmailTaken is returned when a registration email is already in use.
var ErrEmailTaken = errors.New("email already registered")

// Repository handles all database operations for users, coaches, and clients.
type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// CreateUser inserts a new user row and returns the created User.
func (r *Repository) CreateUser(ctx context.Context, u *User) (*User, error) {
	const q = `
		INSERT INTO users (email, password_hash, role, full_name, phone_e164, timezone)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, email, password_hash, role, full_name, phone_e164, timezone,
		          is_verified, created_at, updated_at`

	row := r.db.QueryRow(ctx, q,
		u.Email, u.PasswordHash, u.Role, u.FullName, u.PhoneE164, u.Timezone,
	)
	return scanUser(row)
}

// GetUserByEmail returns an active user by email address (case-insensitive via CITEXT).
func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	const q = `
		SELECT id, email, password_hash, role, full_name, phone_e164, timezone,
		       is_verified, created_at, updated_at
		FROM users
		WHERE email = $1 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, email)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// GetUserByID returns an active user by primary key.
func (r *Repository) GetUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	const q = `
		SELECT id, email, password_hash, role, full_name, phone_e164, timezone,
		       is_verified, created_at, updated_at
		FROM users
		WHERE id = $1 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, id)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// UpdateUser updates mutable fields on a user row.
func (r *Repository) UpdateUser(ctx context.Context, id uuid.UUID, fullName string, phone *string, timezone string) (*User, error) {
	const q = `
		UPDATE users
		SET full_name = $2, phone_e164 = $3, timezone = $4, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, email, password_hash, role, full_name, phone_e164, timezone,
		          is_verified, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, id, fullName, phone, timezone)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// UpdatePassword sets a new password hash for the user.
func (r *Repository) UpdatePassword(ctx context.Context, userID uuid.UUID, hash string) error {
	const q = `UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, q, userID, hash)
	return err
}

// EmailExists returns true if the email is already registered (soft-delete aware).
func (r *Repository) EmailExists(ctx context.Context, email string) (bool, error) {
	const q = `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1 AND deleted_at IS NULL)`
	var exists bool
	err := r.db.QueryRow(ctx, q, email).Scan(&exists)
	return exists, err
}

// --- Coach ---

// CreateCoach inserts a coach profile row linked to the given user.
func (r *Repository) CreateCoach(ctx context.Context, userID uuid.UUID, businessName *string) (*Coach, error) {
	const q = `
		INSERT INTO coaches (user_id, business_name)
		VALUES ($1, $2)
		RETURNING id, user_id, business_name, stripe_account_id, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, userID, businessName)
	return scanCoach(row)
}

// GetCoachByUserID returns the coach profile for a given user.
func (r *Repository) GetCoachByUserID(ctx context.Context, userID uuid.UUID) (*Coach, error) {
	const q = `
		SELECT id, user_id, business_name, stripe_account_id, created_at, updated_at
		FROM coaches WHERE user_id = $1 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, userID)
	c, err := scanCoach(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// GetCoachByID returns the coach profile by coach primary key.
func (r *Repository) GetCoachByID(ctx context.Context, coachID uuid.UUID) (*Coach, error) {
	const q = `
		SELECT id, user_id, business_name, stripe_account_id, created_at, updated_at
		FROM coaches WHERE id = $1 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, coachID)
	c, err := scanCoach(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// UpdateCoach updates the coach's business name.
func (r *Repository) UpdateCoach(ctx context.Context, coachID uuid.UUID, businessName *string) (*Coach, error) {
	const q = `
		UPDATE coaches SET business_name = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, user_id, business_name, stripe_account_id, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, coachID, businessName)
	c, err := scanCoach(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// --- Client ---

// CreateClient inserts a client profile row linked to the given user and coach.
func (r *Repository) CreateClient(ctx context.Context, userID, coachID uuid.UUID, sessionsPerMonth int) (*Client, error) {
	const q = `
		INSERT INTO clients (user_id, coach_id, sessions_per_month)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, coach_id, tenure_started_at, sessions_per_month,
		          priority_score, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, userID, coachID, sessionsPerMonth)
	return scanClient(row)
}

// GetClientByUserID returns the client profile for a given user.
func (r *Repository) GetClientByUserID(ctx context.Context, userID uuid.UUID) (*Client, error) {
	const q = `
		SELECT id, user_id, coach_id, tenure_started_at, sessions_per_month,
		       priority_score, created_at, updated_at
		FROM clients WHERE user_id = $1 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, userID)
	c, err := scanClient(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// GetClientByID returns the client profile by client primary key.
func (r *Repository) GetClientByID(ctx context.Context, clientID uuid.UUID) (*Client, error) {
	const q = `
		SELECT id, user_id, coach_id, tenure_started_at, sessions_per_month,
		       priority_score, created_at, updated_at
		FROM clients WHERE id = $1 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, clientID)
	c, err := scanClient(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// GetClientsByCoachID returns all active clients belonging to a coach.
func (r *Repository) GetClientsByCoachID(ctx context.Context, coachID uuid.UUID) ([]Client, error) {
	const q = `
		SELECT id, user_id, coach_id, tenure_started_at, sessions_per_month,
		       priority_score, created_at, updated_at
		FROM clients
		WHERE coach_id = $1 AND deleted_at IS NULL
		ORDER BY priority_score DESC, tenure_started_at ASC`

	rows, err := r.db.Query(ctx, q, coachID)
	if err != nil {
		return nil, fmt.Errorf("users: get clients by coach: %w", err)
	}
	defer rows.Close()

	var result []Client
	for rows.Next() {
		var c Client
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.CoachID, &c.TenureStartedAt, &c.SessionsPerMonth,
			&c.PriorityScore, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("users: scan client: %w", err)
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// --- Scan helpers ---

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.FullName,
		&u.PhoneE164, &u.Timezone, &u.IsVerified, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("users: scan user: %w", err)
	}
	return &u, nil
}

func scanCoach(row pgx.Row) (*Coach, error) {
	var c Coach
	err := row.Scan(
		&c.ID, &c.UserID, &c.BusinessName, &c.StripeAccountID, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("users: scan coach: %w", err)
	}
	return &c, nil
}

func scanClient(row pgx.Row) (*Client, error) {
	var c Client
	err := row.Scan(
		&c.ID, &c.UserID, &c.CoachID, &c.TenureStartedAt, &c.SessionsPerMonth,
		&c.PriorityScore, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("users: scan client: %w", err)
	}
	return &c, nil
}
