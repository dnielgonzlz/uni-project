package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrTokenNotFound is returned when a token hash has no matching active record.
var ErrTokenNotFound = errors.New("token not found or expired")

// Repository handles token storage for auth flows.
type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// --- Refresh tokens ---

// CreateRefreshToken persists a hashed refresh token.
func (r *Repository) CreateRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) (*RefreshToken, error) {
	const q = `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, token_hash, expires_at, revoked_at, created_at`

	var rt RefreshToken
	err := r.db.QueryRow(ctx, q, userID, tokenHash, expiresAt).Scan(
		&rt.ID, &rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &rt.RevokedAt, &rt.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("auth: create refresh token: %w", err)
	}
	return &rt, nil
}

// GetRefreshToken returns a valid (not expired, not revoked) refresh token by hash.
func (r *Repository) GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	const q = `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM refresh_tokens
		WHERE token_hash = $1
		  AND expires_at > NOW()
		  AND revoked_at IS NULL`

	var rt RefreshToken
	err := r.db.QueryRow(ctx, q, tokenHash).Scan(
		&rt.ID, &rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &rt.RevokedAt, &rt.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: get refresh token: %w", err)
	}
	return &rt, nil
}

// RevokeRefreshToken marks a single refresh token as revoked.
func (r *Repository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	const q = `UPDATE refresh_tokens SET revoked_at = NOW() WHERE token_hash = $1`
	_, err := r.db.Exec(ctx, q, tokenHash)
	return err
}

// RevokeAllRefreshTokens revokes every refresh token for a user (used on password reset).
func (r *Repository) RevokeAllRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	const q = `UPDATE refresh_tokens SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`
	_, err := r.db.Exec(ctx, q, userID)
	return err
}

// --- Password reset tokens ---

// CreatePasswordResetToken persists a hashed password reset token.
func (r *Repository) CreatePasswordResetToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) (*PasswordResetToken, error) {
	const q = `
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, token_hash, expires_at, used_at, created_at`

	var pt PasswordResetToken
	err := r.db.QueryRow(ctx, q, userID, tokenHash, expiresAt).Scan(
		&pt.ID, &pt.UserID, &pt.TokenHash, &pt.ExpiresAt, &pt.UsedAt, &pt.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("auth: create password reset token: %w", err)
	}
	return &pt, nil
}

// GetPasswordResetToken returns a valid (not expired, not used) reset token by hash.
func (r *Repository) GetPasswordResetToken(ctx context.Context, tokenHash string) (*PasswordResetToken, error) {
	const q = `
		SELECT id, user_id, token_hash, expires_at, used_at, created_at
		FROM password_reset_tokens
		WHERE token_hash = $1
		  AND expires_at > NOW()
		  AND used_at IS NULL`

	var pt PasswordResetToken
	err := r.db.QueryRow(ctx, q, tokenHash).Scan(
		&pt.ID, &pt.UserID, &pt.TokenHash, &pt.ExpiresAt, &pt.UsedAt, &pt.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: get password reset token: %w", err)
	}
	return &pt, nil
}

// MarkPasswordResetTokenUsed marks the token as consumed so it cannot be reused.
func (r *Repository) MarkPasswordResetTokenUsed(ctx context.Context, tokenHash string) error {
	const q = `UPDATE password_reset_tokens SET used_at = NOW() WHERE token_hash = $1`
	_, err := r.db.Exec(ctx, q, tokenHash)
	return err
}
