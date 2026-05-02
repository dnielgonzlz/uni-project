package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters, simple encryption for a university server.
// Increase time and memory in production as hardware allows.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword returns an argon2id hash of the password, suitable for storage.
// Format: argon2id$v=19$<time>$<memory>$<threads>$<base64salt>$<base64hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	encoded := fmt.Sprintf("argon2id$v=%d$%d$%d$%d$%s$%s",
		argon2.Version,
		argonTime,
		argonMemory,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
	return encoded, nil
}

// VerifyPassword checks whether password matches the stored hash.
// Returns nil on match, ErrInvalidPassword on mismatch, or a wrapped error on failure.
var ErrInvalidPassword = errors.New("invalid password")

func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 7 || parts[0] != "argon2id" {
		return fmt.Errorf("password: invalid hash format")
	}

	var version, time, memory, threads uint32
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil {
		return fmt.Errorf("password: parse version: %w", err)
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &time); err != nil {
		return fmt.Errorf("password: parse time: %w", err)
	}
	if _, err := fmt.Sscanf(parts[3], "%d", &memory); err != nil {
		return fmt.Errorf("password: parse memory: %w", err)
	}
	if _, err := fmt.Sscanf(parts[4], "%d", &threads); err != nil {
		return fmt.Errorf("password: parse threads: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("password: decode salt: %w", err)
	}
	storedHash, err := base64.RawStdEncoding.DecodeString(parts[6])
	if err != nil {
		return fmt.Errorf("password: decode hash: %w", err)
	}

	computed := argon2.IDKey([]byte(password), salt, time, memory, uint8(threads), uint32(len(storedHash)))

	if subtle.ConstantTimeCompare(computed, storedHash) != 1 {
		return ErrInvalidPassword
	}
	return nil
}

// GenerateSecureToken returns a cryptographically random 32-byte hex string.
// This is the raw token returned to the client, only its SHA-256 hash is stored.
func GenerateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("token: generate: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hex digest of a raw token string.
// Always pass the digest to database queries, never the raw token.
func HashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
