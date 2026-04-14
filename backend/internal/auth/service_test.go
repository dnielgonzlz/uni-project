package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/danielgonzalez/pt-scheduler/internal/auth"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/clock"
)

// --- Password hashing ---

func TestHashAndVerifyPassword(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword("correcthorsebatterystaple")
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	t.Run("correct password verifies", func(t *testing.T) {
		err := auth.VerifyPassword("correcthorsebatterystaple", hash)
		require.NoError(t, err)
	})

	t.Run("wrong password rejected", func(t *testing.T) {
		err := auth.VerifyPassword("wrongpassword", hash)
		require.ErrorIs(t, err, auth.ErrInvalidPassword)
	})

	t.Run("empty password rejected", func(t *testing.T) {
		err := auth.VerifyPassword("", hash)
		require.ErrorIs(t, err, auth.ErrInvalidPassword)
	})
}

func TestHashPasswordProducesUniqueHashes(t *testing.T) {
	t.Parallel()
	// Same password must produce different hashes (different salts).
	h1, err := auth.HashPassword("password123")
	require.NoError(t, err)

	h2, err := auth.HashPassword("password123")
	require.NoError(t, err)

	require.NotEqual(t, h1, h2, "same password should produce different hashes due to unique salt")
}

// --- Secure token generation ---

func TestGenerateSecureToken(t *testing.T) {
	t.Parallel()

	tok1, err := auth.GenerateSecureToken()
	require.NoError(t, err)
	require.Len(t, tok1, 64) // 32 bytes as hex = 64 chars

	tok2, err := auth.GenerateSecureToken()
	require.NoError(t, err)

	require.NotEqual(t, tok1, tok2, "tokens must be unique")
}

func TestHashToken(t *testing.T) {
	t.Parallel()
	raw := "abc123"
	h1 := auth.HashToken(raw)
	h2 := auth.HashToken(raw)
	require.Equal(t, h1, h2, "same input must produce same hash")
	require.NotEqual(t, raw, h1, "hash must differ from input")
}

// --- JWT ---

func TestGenerateAndParseAccessToken(t *testing.T) {
	t.Parallel()

	secret := "test-secret-32-chars-minimum-ok!"
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	token, err := auth.GenerateAccessToken(userID, "coach", secret, 15)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := auth.ParseAccessToken(token, secret)
	require.NoError(t, err)
	require.Equal(t, userID, claims.UserID)
	require.Equal(t, "coach", claims.Role)
}

func TestParseAccessToken_WrongSecret(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	token, err := auth.GenerateAccessToken(userID, "coach", "correct-secret", 15)
	require.NoError(t, err)

	_, err = auth.ParseAccessToken(token, "wrong-secret")
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestParseAccessToken_Expired(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	// Expiry of 0 minutes means the token expires immediately.
	token, err := auth.GenerateAccessToken(userID, "coach", "secret", 0)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	_, err = auth.ParseAccessToken(token, "secret")
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

// --- Clock ---

func TestFixedClock(t *testing.T) {
	t.Parallel()
	fixed := clock.Fixed{T: time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)}
	require.Equal(t, fixed.T, fixed.Now())
}
