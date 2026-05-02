// Package audit provides a thin write-only client for the audit_log table.
// It is intentionally simple: every key action in the system calls Log() and
// the call is fire-and-forget (errors are logged but never returned to the caller).
// The audit log is append-only — rows are never updated or deleted.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Action constants — stored verbatim in audit_log.action.
const (
	ActionLogin              = "login"
	ActionLogout             = "logout"
	ActionRegister           = "register"
	ActionPasswordReset      = "password_reset"
	ActionScheduleConfirmed  = "schedule_confirmed"
	ActionSessionCancelled   = "session_cancelled"
	ActionPaymentCharged     = "payment_charged"
	ActionPaymentFailed      = "payment_failed"
	ActionMandateCreated     = "mandate_created"
	ActionDataExport         = "data_export"
	ActionClientDeleted      = "client_deleted"
)

// Logger writes entries to the audit_log table.
type Logger struct {
	db     *pgxpool.Pool
	logger *slog.Logger
}

func NewLogger(db *pgxpool.Pool, logger *slog.Logger) *Logger {
	return &Logger{db: db, logger: logger}
}

// Log writes one audit entry. It is safe to call from any goroutine.
// userID and entityID are optional (pass nil when not applicable).
// detail is any JSON-serialisable value for extra context; pass nil to omit.
func (a *Logger) Log(ctx context.Context, userID *uuid.UUID, action, entityType string, entityID *uuid.UUID, detail any, ipAddress string) {
	var detailJSON []byte
	if detail != nil {
		b, err := json.Marshal(detail)
		if err == nil {
			detailJSON = b
		}
	}

	const q = `
		INSERT INTO audit_log (user_id, action, entity_type, entity_id, detail, ip_address)
		VALUES ($1, $2, $3, $4, $5, $6)`

	if _, err := a.db.Exec(ctx, q, userID, action, entityType, entityID, detailJSON, ipAddress); err != nil {
		// Audit failures must never break application flow.
		a.logger.WarnContext(ctx, "audit log write failed",
			"action", action,
			"entity_type", entityType,
			"error", err,
		)
	}
}

// IPFromRequest extracts the real client IP, preferring X-Forwarded-For
// (set by the Elastic Beanstalk load balancer) over RemoteAddr.
func IPFromRequest(remoteAddr, forwardedFor string) string {
	if forwardedFor != "" {
		// X-Forwarded-For can be a comma-separated list; first entry is the client
		for i := 0; i < len(forwardedFor); i++ {
			if forwardedFor[i] == ',' {
				return forwardedFor[:i]
			}
		}
		return forwardedFor
	}
	// Strip port from RemoteAddr
	for i := len(remoteAddr) - 1; i >= 0; i-- {
		if remoteAddr[i] == ':' {
			return remoteAddr[:i]
		}
	}
	return remoteAddr
}
