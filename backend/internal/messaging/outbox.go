package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OutboxEvent types — these constants are stored in notification_outbox.event_type.
const (
	EventSessionConfirmed       = "session_confirmed"
	EventSessionReminder        = "session_reminder"
	EventSessionCancelled       = "session_cancelled"
	EventCancellationPending    = "cancellation_pending"
	EventPaymentFailed          = "payment_failed"
)

// OutboxEntry is a single row in notification_outbox.
type OutboxEntry struct {
	ID           uuid.UUID       `json:"id"`
	EventType    string          `json:"event_type"`
	Payload      json.RawMessage `json:"payload"`
	Status       string          `json:"status"`
	Attempts     int             `json:"attempts"`
	LastError    *string         `json:"last_error,omitempty"`
	ProcessAfter time.Time       `json:"process_after"`
	CreatedAt    time.Time       `json:"created_at"`
}

// SessionConfirmedPayload is the JSON stored for EventSessionConfirmed and EventSessionReminder.
type SessionConfirmedPayload struct {
	SessionID  uuid.UUID `json:"session_id"`
	ClientID   uuid.UUID `json:"client_id"`
	CoachID    uuid.UUID `json:"coach_id"`
	StartsAt   time.Time `json:"starts_at"`
	ClientName string    `json:"client_name"`
	ClientPhone string   `json:"client_phone"`
	ClientEmail string   `json:"client_email"`
	CoachName  string    `json:"coach_name"`
	CoachEmail string    `json:"coach_email"`
}

// SessionCancelledPayload is the JSON stored for EventSessionCancelled.
type SessionCancelledPayload struct {
	SessionID   uuid.UUID `json:"session_id"`
	ClientID    uuid.UUID `json:"client_id"`
	StartsAt    time.Time `json:"starts_at"`
	ClientName  string    `json:"client_name"`
	ClientPhone string    `json:"client_phone"`
	ClientEmail string    `json:"client_email"`
	CreditIssued bool     `json:"credit_issued"`
}

// CancellationPendingPayload is the JSON stored for EventCancellationPending.
// Sent to the coach when a client cancels inside the 24h window.
type CancellationPendingPayload struct {
	SessionID          uuid.UUID `json:"session_id"`
	ClientID           uuid.UUID `json:"client_id"`
	CoachID            uuid.UUID `json:"coach_id"`
	StartsAt           time.Time `json:"starts_at"`
	ClientName         string    `json:"client_name"`
	CancellationReason string    `json:"cancellation_reason"`
	CoachName          string    `json:"coach_name"`
	CoachEmail         string    `json:"coach_email"`
	CoachPhone         string    `json:"coach_phone"`
}

// PaymentFailedPayload is the JSON stored for EventPaymentFailed.
type PaymentFailedPayload struct {
	ClientID     uuid.UUID `json:"client_id"`
	CoachID      uuid.UUID `json:"coach_id"`
	ClientName   string    `json:"client_name"`
	CoachName    string    `json:"coach_name"`
	CoachEmail   string    `json:"coach_email"`
	CoachPhone   string    `json:"coach_phone"`
	BillingYear  int       `json:"billing_year"`
	BillingMonth int       `json:"billing_month"`
	Provider     string    `json:"provider"`
}

// OutboxRepository handles DB operations for the notification outbox.
type OutboxRepository struct {
	db *pgxpool.Pool
}

func NewOutboxRepository(db *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// Enqueue inserts a new outbox entry to be processed by the worker.
// processAfter allows scheduling delayed notifications (e.g. 24h-before reminder).
func (r *OutboxRepository) Enqueue(ctx context.Context, eventType string, payload any, processAfter time.Time) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outbox: marshal payload: %w", err)
	}

	const q = `
		INSERT INTO notification_outbox (event_type, payload, process_after)
		VALUES ($1, $2, $3)`

	if _, err := r.db.Exec(ctx, q, eventType, raw, processAfter); err != nil {
		return fmt.Errorf("outbox: enqueue %s: %w", eventType, err)
	}
	return nil
}

// ClaimBatch atomically claims up to `limit` pending entries whose process_after
// is in the past, marking them as 'processing'. Returns the claimed entries.
// Uses SELECT … FOR UPDATE SKIP LOCKED so multiple worker instances don't double-process.
func (r *OutboxRepository) ClaimBatch(ctx context.Context, limit int) ([]OutboxEntry, error) {
	const q = `
		UPDATE notification_outbox
		SET status = 'processing', attempts = attempts + 1, updated_at = NOW()
		WHERE id IN (
			SELECT id FROM notification_outbox
			WHERE status = 'pending' AND process_after <= NOW()
			ORDER BY process_after ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, event_type, payload, status, attempts, last_error, process_after, created_at`

	rows, err := r.db.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("outbox: claim batch: %w", err)
	}
	defer rows.Close()

	var entries []OutboxEntry
	for rows.Next() {
		var e OutboxEntry
		if err := rows.Scan(
			&e.ID, &e.EventType, &e.Payload, &e.Status,
			&e.Attempts, &e.LastError, &e.ProcessAfter, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("outbox: scan entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// MarkDone marks an outbox entry as successfully delivered.
func (r *OutboxRepository) MarkDone(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE notification_outbox SET status = 'done', updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id)
	return err
}

// MarkFailed records a delivery failure. After maxAttempts the status is set to
// 'failed' permanently; otherwise it resets to 'pending' for a retry.
func (r *OutboxRepository) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string, maxAttempts int) error {
	const q = `
		UPDATE notification_outbox
		SET status      = CASE WHEN attempts >= $3 THEN 'failed' ELSE 'pending' END,
		    last_error  = $2,
		    updated_at  = NOW()
		WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id, errMsg, maxAttempts)
	return err
}
