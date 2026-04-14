package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

const (
	contextKeyUserID contextKey = "user_id"
	contextKeyRole   contextKey = "role"
)

// Middleware returns an HTTP middleware that validates the Bearer JWT on each request.
// Requests without a valid token receive 401. Attach this to protected route groups.
func Middleware(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				// FRONTEND: redirect to login page on 401
				httpx.Error(w, http.StatusUnauthorized, "missing or invalid authorisation header")
				return
			}

			tokenStr := strings.TrimPrefix(header, "Bearer ")
			claims, err := ParseAccessToken(tokenStr, jwtSecret)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), contextKeyUserID, claims.UserID)
			ctx = context.WithValue(ctx, contextKeyRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole returns middleware that allows only users with one of the specified roles.
// Must be used after Middleware (JWT must already be validated).
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := r.Context().Value(contextKeyRole).(string)
			if !ok {
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			if _, permitted := allowed[role]; !permitted {
				// FRONTEND: show "you don't have permission" message
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserIDFromContext extracts the authenticated user's UUID from the request context.
// Returns the zero UUID and false if not set.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(contextKeyUserID).(uuid.UUID)
	return id, ok
}

// RoleFromContext extracts the authenticated user's role from the request context.
func RoleFromContext(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(contextKeyRole).(string)
	return role, ok
}
