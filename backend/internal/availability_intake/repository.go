package availability_intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrDuplicateWebhook is returned when Twilio retries a message we already handled.
var ErrDuplicateWebhook = errors.New("webhook event already processed")

// Repository manages Twilio conversation state.
type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// GetOrCreateConversation returns the existing conversation for a client or creates one in idle state.
func (r *Repository) GetOrCreateConversation(ctx context.Context, clientID uuid.UUID) (*Conversation, error) {
	const upsert = `
		INSERT INTO availability_intake_conversations (client_id, state, context)
		VALUES ($1, 'idle', '{}')
		ON CONFLICT (client_id) DO NOTHING`
	_, _ = r.db.Exec(ctx, upsert, clientID)

	const sel = `
		SELECT id, client_id, state, context, started_at, updated_at
		FROM availability_intake_conversations WHERE client_id = $1`

	return r.scan(r.db.QueryRow(ctx, sel, clientID))
}

// UpdateConversation persists the new state and context.
func (r *Repository) UpdateConversation(ctx context.Context, id uuid.UUID, state string, ctx2 map[string]any) (*Conversation, error) {
	ctxJSON, err := json.Marshal(ctx2)
	if err != nil {
		return nil, fmt.Errorf("intake: marshal context: %w", err)
	}

	const q = `
		UPDATE availability_intake_conversations
		SET state = $2, context = $3, updated_at = NOW(),
		    started_at = COALESCE(started_at, NOW())
		WHERE id = $1
		RETURNING id, client_id, state, context, started_at, updated_at`

	return r.scan(r.db.QueryRow(ctx, q, id, state, ctxJSON))
}

// GetClientIDByPhone returns the client UUID associated with a phone number.
func (r *Repository) GetClientIDByPhone(ctx context.Context, phoneE164 string) (uuid.UUID, error) {
	client, err := r.GetClientByPhone(ctx, phoneE164)
	if err != nil {
		return uuid.UUID{}, err
	}
	return client.ID, nil
}

// GetClientByPhone returns the client associated with a phone number and whether
// the coach has enabled the MVP AI booking agent for them.
func (r *Repository) GetClientByPhone(ctx context.Context, phoneE164 string) (*InboundClient, error) {
	const q = `
		SELECT c.id, c.ai_booking_enabled FROM clients c
		JOIN users u ON u.id = c.user_id
		WHERE u.phone_e164 = $1 AND c.deleted_at IS NULL AND u.deleted_at IS NULL
		LIMIT 1`

	var client InboundClient
	err := r.db.QueryRow(ctx, q, phoneE164).Scan(&client.ID, &client.AIBookingEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("intake: client not found for phone %s", phoneE164)
	}
	return &client, err
}

// RecordWebhookEvent stores Twilio's MessageSid so retries do not process twice.
func (r *Repository) RecordWebhookEvent(ctx context.Context, messageSID string, payload map[string][]string) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("intake: marshal webhook payload: %w", err)
	}

	const q = `
		INSERT INTO webhook_events (provider, event_id, payload)
		VALUES ('twilio', $1, $2)
		ON CONFLICT (provider, event_id) DO NOTHING`

	tag, err := r.db.Exec(ctx, q, messageSID, payloadJSON)
	if err != nil {
		return fmt.Errorf("intake: record twilio webhook: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDuplicateWebhook
	}
	return nil
}

func (r *Repository) MarkLatestRecipientReplied(ctx context.Context, clientID uuid.UUID, rawReply string) error {
	const q = `
		UPDATE availability_campaign_recipients
		SET status = 'replied', replied_at = NOW(), raw_reply = $2, updated_at = NOW()
		WHERE id = (
			SELECT acr.id
			FROM availability_campaign_recipients acr
			JOIN availability_campaigns ac ON ac.id = acr.campaign_id
			WHERE acr.client_id = $1
			  AND acr.status IN ('pending', 'sent')
			  AND ac.status IN ('scheduled', 'sent')
			ORDER BY ac.week_start DESC, acr.created_at DESC
			LIMIT 1
		)`
	if _, err := r.db.Exec(ctx, q, clientID, rawReply); err != nil {
		return fmt.Errorf("intake: mark latest campaign recipient replied: %w", err)
	}
	return nil
}

// GetLatestCampaignWeekStart returns the week_start of the most recent active campaign
// for the given client. Used by the AI parser to resolve relative day names correctly.
func (r *Repository) GetLatestCampaignWeekStart(ctx context.Context, clientID uuid.UUID) (time.Time, error) {
	const q = `
		SELECT ac.week_start::timestamptz
		FROM availability_campaign_recipients acr
		JOIN availability_campaigns ac ON ac.id = acr.campaign_id
		WHERE acr.client_id = $1
		  AND acr.status IN ('pending', 'sent', 'replied')
		  AND ac.status IN ('scheduled', 'sent')
		ORDER BY ac.week_start DESC, acr.created_at DESC
		LIMIT 1`
	var weekStart time.Time
	if err := r.db.QueryRow(ctx, q, clientID).Scan(&weekStart); err != nil {
		return time.Time{}, fmt.Errorf("intake: get campaign week start: %w", err)
	}
	return weekStart, nil
}

// UpdateRecipientParseResult stores the AI parse outcome on the most recently replied
// recipient row for the client. Failures here are non-fatal — windows are already saved.
func (r *Repository) UpdateRecipientParseResult(ctx context.Context, clientID uuid.UUID, parseStatus string, parsedWindowsJSON []byte) error {
	const q = `
		UPDATE availability_campaign_recipients
		SET parse_status    = $2,
		    parsed_windows  = $3,
		    updated_at      = NOW()
		WHERE id = (
			SELECT acr.id
			FROM availability_campaign_recipients acr
			JOIN availability_campaigns ac ON ac.id = acr.campaign_id
			WHERE acr.client_id = $1
			  AND acr.status = 'replied'
			ORDER BY ac.week_start DESC, acr.replied_at DESC
			LIMIT 1
		)`
	if _, err := r.db.Exec(ctx, q, clientID, parseStatus, parsedWindowsJSON); err != nil {
		return fmt.Errorf("intake: update recipient parse result: %w", err)
	}
	return nil
}

func (r *Repository) scan(row pgx.Row) (*Conversation, error) {
	var c Conversation
	var ctxJSON []byte
	err := row.Scan(&c.ID, &c.ClientID, &c.State, &ctxJSON, &c.StartedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("intake: scan conversation: %w", err)
	}
	if err := json.Unmarshal(ctxJSON, &c.Context); err != nil {
		c.Context = map[string]any{}
	}
	return &c, nil
}
