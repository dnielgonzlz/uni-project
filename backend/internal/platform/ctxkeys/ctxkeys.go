// Package ctxkeys defines the shared context keys used by the auth middleware
// and read by any handler that needs the authenticated user's identity.
// Both auth and domain handler packages can import this without import cycles.
package ctxkeys

import "github.com/google/uuid"

type contextKey string

const (
	UserIDKey contextKey = "user_id"
	RoleKey   contextKey = "role"
)

// UserIDFromContext returns the authenticated user's UUID from the request context.
// Returns uuid.Nil if the key is not set (i.e. unauthenticated request).
func UserIDFromContext(ctx interface{ Value(any) any }) uuid.UUID {
	v := ctx.Value(UserIDKey)
	if v == nil {
		return uuid.Nil
	}
	id, _ := v.(uuid.UUID)
	return id
}

// RoleFromContext returns the authenticated user's role string from the request context.
func RoleFromContext(ctx interface{ Value(any) any }) string {
	v := ctx.Value(RoleKey)
	if v == nil {
		return ""
	}
	r, _ := v.(string)
	return r
}
