package availability_intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository manages SMS conversation state.
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
	const q = `
		SELECT c.id FROM clients c
		JOIN users u ON u.id = c.user_id
		WHERE u.phone_e164 = $1 AND c.deleted_at IS NULL AND u.deleted_at IS NULL
		LIMIT 1`

	var id uuid.UUID
	err := r.db.QueryRow(ctx, q, phoneE164).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, fmt.Errorf("intake: client not found for phone %s", phoneE164)
	}
	return id, err
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
