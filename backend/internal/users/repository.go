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

// ErrForbidden is returned when a user tries to act on a resource they do not own.
var ErrForbidden = errors.New("forbidden")

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
		          is_verified, calendar_token, created_at, updated_at`

	row := r.db.QueryRow(ctx, q,
		u.Email, u.PasswordHash, u.Role, u.FullName, u.PhoneE164, u.Timezone,
	)
	return scanUser(row)
}

// GetUserByEmail returns an active user by email address (case-insensitive via CITEXT).
func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	const q = `
		SELECT id, email, password_hash, role, full_name, phone_e164, timezone,
		       is_verified, calendar_token, created_at, updated_at
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
		       is_verified, calendar_token, created_at, updated_at
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
		          is_verified, calendar_token, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, id, fullName, phone, timezone)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// MarkVerified sets is_verified = true for the given user.
func (r *Repository) MarkVerified(ctx context.Context, userID uuid.UUID) error {
	const q = `UPDATE users SET is_verified = true, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, q, userID)
	return err
}

// GetUserByCalendarToken looks up a user by their opaque calendar subscription token.
// Used by the public ICS feed endpoint — no JWT involved.
func (r *Repository) GetUserByCalendarToken(ctx context.Context, token uuid.UUID) (*User, error) {
	const q = `
		SELECT id, email, password_hash, role, full_name, phone_e164, timezone,
		       is_verified, calendar_token, created_at, updated_at
		FROM users
		WHERE calendar_token = $1 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, token)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// RegenerateCalendarToken replaces the calendar token with a new random UUID.
// Call this when the user wants to revoke their existing subscription URL.
func (r *Repository) RegenerateCalendarToken(ctx context.Context, userID uuid.UUID) (*User, error) {
	const q = `
		UPDATE users
		SET calendar_token = gen_random_uuid(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, email, password_hash, role, full_name, phone_e164, timezone,
		          is_verified, calendar_token, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, userID)
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
		RETURNING id, user_id, business_name, stripe_account_id, max_sessions_per_day, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, userID, businessName)
	return scanCoach(row)
}

// GetCoachByUserID returns the coach profile for a given user.
func (r *Repository) GetCoachByUserID(ctx context.Context, userID uuid.UUID) (*Coach, error) {
	const q = `
		SELECT id, user_id, business_name, stripe_account_id, max_sessions_per_day, created_at, updated_at
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
		SELECT id, user_id, business_name, stripe_account_id, max_sessions_per_day, created_at, updated_at
		FROM coaches WHERE id = $1 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, coachID)
	c, err := scanCoach(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// UpdateCoach updates the coach's business name and max sessions per day.
func (r *Repository) UpdateCoach(ctx context.Context, coachID uuid.UUID, businessName *string, maxSessionsPerDay int) (*Coach, error) {
	const q = `
		UPDATE coaches SET business_name = $2, max_sessions_per_day = $3, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, user_id, business_name, stripe_account_id, max_sessions_per_day, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, coachID, businessName, maxSessionsPerDay)
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
		INSERT INTO clients (user_id, coach_id, sessions_per_month, ai_booking_enabled)
		VALUES ($1, $2, $3, TRUE)
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

// SoftDeleteClientForCoach deactivates a client/user pair owned by the given coach and
// soft-deletes any future active sessions so the client disappears from active operations.
func (r *Repository) SoftDeleteClientForCoach(ctx context.Context, coachID, clientID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("users: begin delete client tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const ownershipQ = `
		SELECT user_id
		FROM clients
		WHERE id = $1 AND deleted_at IS NULL`

	var userID uuid.UUID
	if err := tx.QueryRow(ctx, ownershipQ, clientID).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("users: fetch client for delete: %w", err)
	}

	const verifyQ = `
		SELECT 1
		FROM clients
		WHERE id = $1 AND coach_id = $2 AND deleted_at IS NULL`

	var owned int
	if err := tx.QueryRow(ctx, verifyQ, clientID, coachID).Scan(&owned); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrForbidden
		}
		return fmt.Errorf("users: verify client ownership: %w", err)
	}

	const deleteFutureSessionsQ = `
		UPDATE sessions
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE client_id = $1
		  AND deleted_at IS NULL
		  AND status IN ('proposed', 'confirmed', 'pending_cancellation')
		  AND starts_at >= NOW()`

	if _, err := tx.Exec(ctx, deleteFutureSessionsQ, clientID); err != nil {
		return fmt.Errorf("users: soft delete client sessions: %w", err)
	}

	const deleteClientQ = `
		UPDATE clients
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`

	if _, err := tx.Exec(ctx, deleteClientQ, clientID); err != nil {
		return fmt.Errorf("users: soft delete client: %w", err)
	}

	const deleteUserQ = `
		UPDATE users
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`

	if _, err := tx.Exec(ctx, deleteUserQ, userID); err != nil {
		return fmt.Errorf("users: soft delete user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("users: commit delete client: %w", err)
	}

	return nil
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

// ListCoachClientSummaries returns coach-owned clients with embedded user data and confirmed session counts.
func (r *Repository) ListCoachClientSummaries(ctx context.Context, coachID uuid.UUID) ([]CoachClientSummary, error) {
	const q = `
		SELECT
			u.id, u.email, u.password_hash, u.role, u.full_name, u.phone_e164, u.timezone,
			u.is_verified, u.calendar_token, u.created_at, u.updated_at,
			c.id, c.user_id, c.coach_id, c.tenure_started_at, c.sessions_per_month,
			c.priority_score, c.created_at, c.updated_at,
			COALESCE(COUNT(s.id), 0) AS confirmed_session_count
		FROM clients c
		JOIN users u
		  ON u.id = c.user_id
		 AND u.deleted_at IS NULL
		LEFT JOIN sessions s
		  ON s.client_id = c.id
		 AND s.status = 'confirmed'
		 AND s.deleted_at IS NULL
		WHERE c.coach_id = $1
		  AND c.deleted_at IS NULL
		GROUP BY
			u.id, u.email, u.password_hash, u.role, u.full_name, u.phone_e164, u.timezone,
			u.is_verified, u.calendar_token, u.created_at, u.updated_at,
			c.id, c.user_id, c.coach_id, c.tenure_started_at, c.sessions_per_month,
			c.priority_score, c.created_at, c.updated_at
		ORDER BY u.full_name ASC`

	rows, err := r.db.Query(ctx, q, coachID)
	if err != nil {
		return nil, fmt.Errorf("users: list coach client summaries: %w", err)
	}
	defer rows.Close()

	var result []CoachClientSummary
	for rows.Next() {
		var item CoachClientSummary
		if err := rows.Scan(
			&item.User.ID, &item.User.Email, &item.User.PasswordHash, &item.User.Role, &item.User.FullName,
			&item.User.PhoneE164, &item.User.Timezone, &item.User.IsVerified, &item.User.CalendarToken,
			&item.User.CreatedAt, &item.User.UpdatedAt,
			&item.Client.ID, &item.Client.UserID, &item.Client.CoachID, &item.Client.TenureStartedAt,
			&item.Client.SessionsPerMonth, &item.Client.PriorityScore, &item.Client.CreatedAt,
			&item.Client.UpdatedAt, &item.ConfirmedSessionCount,
		); err != nil {
			return nil, fmt.Errorf("users: scan coach client summary: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// GetUserDataExport fetches sessions and payments for a user.
// Uses raw SQL joins to avoid importing the scheduling/billing packages.
// Returns slices of maps (plain JSON-serialisable rows).
func (r *Repository) GetUserDataExport(ctx context.Context, userID uuid.UUID, role string) (sessions, payments []map[string]any, err error) {
	// Sessions: coach sees their coaching sessions; client sees their training sessions.
	var sessionQ string
	if role == RoleCoach {
		sessionQ = `
			SELECT s.id, s.client_id, s.starts_at, s.ends_at, s.status, s.created_at
			FROM sessions s
			JOIN coaches c ON c.id = s.coach_id
			WHERE c.user_id = $1 AND s.deleted_at IS NULL
			ORDER BY s.starts_at DESC`
	} else {
		sessionQ = `
			SELECT s.id, s.coach_id, s.starts_at, s.ends_at, s.status, s.created_at
			FROM sessions s
			JOIN clients cl ON cl.id = s.client_id
			WHERE cl.user_id = $1 AND s.deleted_at IS NULL
			ORDER BY s.starts_at DESC`
	}

	sRows, err := r.db.Query(ctx, sessionQ, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("users: export sessions: %w", err)
	}
	defer sRows.Close()
	for sRows.Next() {
		vals, err := sRows.Values()
		if err != nil {
			return nil, nil, err
		}
		cols := sRows.FieldDescriptions()
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[string(col.Name)] = vals[i]
		}
		sessions = append(sessions, row)
	}
	if err := sRows.Err(); err != nil {
		return nil, nil, err
	}

	// Payments: only clients have payment records.
	if role == RoleClient {
		const payQ = `
			SELECT p.id, p.provider, p.amount_pence, p.currency,
			       p.billing_year, p.billing_month, p.status, p.created_at
			FROM payments p
			JOIN clients cl ON cl.id = p.client_id
			WHERE cl.user_id = $1
			ORDER BY p.billing_year DESC, p.billing_month DESC`

		pRows, err := r.db.Query(ctx, payQ, userID)
		if err != nil {
			return nil, nil, fmt.Errorf("users: export payments: %w", err)
		}
		defer pRows.Close()
		for pRows.Next() {
			vals, err := pRows.Values()
			if err != nil {
				return nil, nil, err
			}
			cols := pRows.FieldDescriptions()
			row := make(map[string]any, len(cols))
			for i, col := range cols {
				row[string(col.Name)] = vals[i]
			}
			payments = append(payments, row)
		}
		if err := pRows.Err(); err != nil {
			return nil, nil, err
		}
	}

	return sessions, payments, nil
}

// --- Scan helpers ---

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.FullName,
		&u.PhoneE164, &u.Timezone, &u.IsVerified, &u.CalendarToken,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("users: scan user: %w", err)
	}
	return &u, nil
}

func scanCoach(row pgx.Row) (*Coach, error) {
	var c Coach
	err := row.Scan(
		&c.ID, &c.UserID, &c.BusinessName, &c.StripeAccountID, &c.MaxSessionsPerDay, &c.CreatedAt, &c.UpdatedAt,
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
