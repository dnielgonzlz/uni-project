package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")
var ErrForbidden = errors.New("forbidden")

type Repository struct {
	db *pgxpool.Pool
}

type CampaignRecipient struct {
	ID     uuid.UUID
	Status string
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetOrCreateSettings(ctx context.Context, coachID uuid.UUID) (*Settings, error) {
	const upsert = `
		INSERT INTO coach_agent_settings (coach_id)
		VALUES ($1)
		ON CONFLICT (coach_id) DO NOTHING`
	if _, err := r.db.Exec(ctx, upsert, coachID); err != nil {
		return nil, fmt.Errorf("agent: create default settings: %w", err)
	}

	const q = `
		SELECT coach_id, enabled, template_sid, template_status,
		       prompt_day, prompt_time::text, timezone, require_coach_confirmation,
		       created_at, updated_at
		FROM coach_agent_settings
		WHERE coach_id = $1`

	return scanSettings(r.db.QueryRow(ctx, q, coachID))
}

func (r *Repository) UpdateSettings(ctx context.Context, coachID uuid.UUID, req UpdateSettingsRequest) (*Settings, error) {
	templateSID := cleanTemplateSID(req.TemplateSID)
	templateStatus := TemplateStatusMissing
	if templateSID != nil {
		templateStatus = TemplateStatusPending
	}

	const q = `
		INSERT INTO coach_agent_settings (coach_id, enabled, template_sid, template_status)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (coach_id) DO UPDATE
		  SET enabled = EXCLUDED.enabled,
		      template_sid = EXCLUDED.template_sid,
		      template_status = EXCLUDED.template_status,
		      updated_at = NOW()
		RETURNING coach_id, enabled, template_sid, template_status,
		          prompt_day, prompt_time::text, timezone, require_coach_confirmation,
		          created_at, updated_at`

	return scanSettings(r.db.QueryRow(ctx, q, coachID, req.Enabled, templateSID, templateStatus))
}

func (r *Repository) UpdateTemplateStatus(ctx context.Context, coachID uuid.UUID, status string) (*Settings, error) {
	const q = `
		UPDATE coach_agent_settings
		SET template_status = $2, updated_at = NOW()
		WHERE coach_id = $1
		RETURNING coach_id, enabled, template_sid, template_status,
		          prompt_day, prompt_time::text, timezone, require_coach_confirmation,
		          created_at, updated_at`

	settings, err := scanSettings(r.db.QueryRow(ctx, q, coachID, status))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return settings, err
}

func (r *Repository) ListClients(ctx context.Context, coachID uuid.UUID) ([]AgentClient, error) {
	const q = `
		SELECT c.id, u.full_name, u.email, u.phone_e164, c.ai_booking_enabled
		FROM clients c
		JOIN users u ON u.id = c.user_id AND u.deleted_at IS NULL
		WHERE c.coach_id = $1 AND c.deleted_at IS NULL
		ORDER BY u.full_name ASC`

	rows, err := r.db.Query(ctx, q, coachID)
	if err != nil {
		return nil, fmt.Errorf("agent: list clients: %w", err)
	}
	defer rows.Close()

	var clients []AgentClient
	for rows.Next() {
		var c AgentClient
		if err := rows.Scan(&c.ClientID, &c.FullName, &c.Email, &c.Phone, &c.AIBookingEnabled); err != nil {
			return nil, fmt.Errorf("agent: scan client: %w", err)
		}
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

func (r *Repository) UpdateClientEnabled(ctx context.Context, coachID, clientID uuid.UUID, enabled bool) (*AgentClient, error) {
	const q = `
		UPDATE clients c
		SET ai_booking_enabled = $3, updated_at = NOW()
		FROM users u
		WHERE c.user_id = u.id
		  AND c.id = $1
		  AND c.coach_id = $2
		  AND c.deleted_at IS NULL
		  AND u.deleted_at IS NULL
		RETURNING c.id, u.full_name, u.email, u.phone_e164, c.ai_booking_enabled`

	var c AgentClient
	err := r.db.QueryRow(ctx, q, clientID, coachID, enabled).Scan(
		&c.ClientID, &c.FullName, &c.Email, &c.Phone, &c.AIBookingEnabled,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrForbidden
	}
	if err != nil {
		return nil, fmt.Errorf("agent: update client enabled: %w", err)
	}
	return &c, nil
}

func (r *Repository) ListCampaignCoaches(ctx context.Context) ([]CampaignCoach, error) {
	const q = `
		SELECT coach_id, template_sid
		FROM coach_agent_settings
		WHERE enabled = TRUE
		  AND template_status = 'approved'
		  AND template_sid IS NOT NULL`

	rows, err := r.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("agent: list campaign coaches: %w", err)
	}
	defer rows.Close()

	var coaches []CampaignCoach
	for rows.Next() {
		var coach CampaignCoach
		if err := rows.Scan(&coach.CoachID, &coach.TemplateSID); err != nil {
			return nil, fmt.Errorf("agent: scan campaign coach: %w", err)
		}
		coaches = append(coaches, coach)
	}
	return coaches, rows.Err()
}

func (r *Repository) CreateCampaign(ctx context.Context, coachID uuid.UUID, weekStart, sendAt time.Time) (uuid.UUID, bool, error) {
	const insertQ = `
		INSERT INTO availability_campaigns (coach_id, week_start, send_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (coach_id, week_start) DO NOTHING
		RETURNING id`

	var campaignID uuid.UUID
	err := r.db.QueryRow(ctx, insertQ, coachID, weekStart, sendAt).Scan(&campaignID)
	if err == nil {
		return campaignID, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, false, fmt.Errorf("agent: create campaign: %w", err)
	}

	const selectQ = `
		SELECT id
		FROM availability_campaigns
		WHERE coach_id = $1 AND week_start = $2`
	if err := r.db.QueryRow(ctx, selectQ, coachID, weekStart).Scan(&campaignID); err != nil {
		return uuid.UUID{}, false, fmt.Errorf("agent: get existing campaign: %w", err)
	}
	return campaignID, false, nil
}

func (r *Repository) ListCampaignClients(ctx context.Context, coachID uuid.UUID) ([]CampaignClient, error) {
	const q = `
		SELECT c.id, u.full_name, u.phone_e164
		FROM clients c
		JOIN users u ON u.id = c.user_id AND u.deleted_at IS NULL
		WHERE c.coach_id = $1
		  AND c.deleted_at IS NULL
		  AND c.ai_booking_enabled = TRUE
		  AND u.phone_e164 IS NOT NULL
		  AND u.phone_e164 <> ''
		ORDER BY u.full_name ASC`

	rows, err := r.db.Query(ctx, q, coachID)
	if err != nil {
		return nil, fmt.Errorf("agent: list campaign clients: %w", err)
	}
	defer rows.Close()

	var clients []CampaignClient
	for rows.Next() {
		var client CampaignClient
		if err := rows.Scan(&client.ClientID, &client.FullName, &client.Phone); err != nil {
			return nil, fmt.Errorf("agent: scan campaign client: %w", err)
		}
		clients = append(clients, client)
	}
	return clients, rows.Err()
}

func (r *Repository) GetOrCreateCampaignRecipient(ctx context.Context, campaignID, clientID uuid.UUID) (*CampaignRecipient, error) {
	const insertQ = `
		INSERT INTO availability_campaign_recipients (campaign_id, client_id)
		VALUES ($1, $2)
		ON CONFLICT (campaign_id, client_id) DO NOTHING
		RETURNING id, status`

	var recipient CampaignRecipient
	err := r.db.QueryRow(ctx, insertQ, campaignID, clientID).Scan(&recipient.ID, &recipient.Status)
	if err == nil {
		return &recipient, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("agent: create campaign recipient: %w", err)
	}

	const selectQ = `
		SELECT id, status
		FROM availability_campaign_recipients
		WHERE campaign_id = $1 AND client_id = $2`
	if err := r.db.QueryRow(ctx, selectQ, campaignID, clientID).Scan(&recipient.ID, &recipient.Status); err != nil {
		return nil, fmt.Errorf("agent: get existing campaign recipient: %w", err)
	}
	return &recipient, nil
}

func (r *Repository) MarkRecipientSent(ctx context.Context, recipientID uuid.UUID, messageSID string) error {
	const q = `
		UPDATE availability_campaign_recipients
		SET status = 'sent', message_sid = $2, sent_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status IN ('pending', 'failed')`
	if _, err := r.db.Exec(ctx, q, recipientID, messageSID); err != nil {
		return fmt.Errorf("agent: mark recipient sent: %w", err)
	}
	return nil
}

func (r *Repository) MarkRecipientFailed(ctx context.Context, recipientID uuid.UUID) error {
	const q = `
		UPDATE availability_campaign_recipients
		SET status = 'failed', updated_at = NOW()
		WHERE id = $1 AND status = 'pending'`
	if _, err := r.db.Exec(ctx, q, recipientID); err != nil {
		return fmt.Errorf("agent: mark recipient failed: %w", err)
	}
	return nil
}

func (r *Repository) MarkCampaignSent(ctx context.Context, campaignID uuid.UUID) error {
	const q = `
		UPDATE availability_campaigns
		SET status = 'sent', updated_at = NOW()
		WHERE id = $1`
	if _, err := r.db.Exec(ctx, q, campaignID); err != nil {
		return fmt.Errorf("agent: mark campaign sent: %w", err)
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
		return fmt.Errorf("agent: mark latest recipient replied: %w", err)
	}
	return nil
}

func (r *Repository) GetOverview(ctx context.Context, coachID uuid.UUID) (*CampaignOverview, error) {
	const campaignQ = `
		SELECT id, status, week_start::timestamptz
		FROM availability_campaigns
		WHERE coach_id = $1
		ORDER BY week_start DESC
		LIMIT 1`

	var campaignID uuid.UUID
	var overview CampaignOverview
	var weekStart time.Time
	err := r.db.QueryRow(ctx, campaignQ, coachID).Scan(&campaignID, &overview.CampaignStatus, &weekStart)
	if errors.Is(err, pgx.ErrNoRows) {
		overview.CampaignStatus = "not_started"
		return &overview, nil
	}
	if err != nil {
		return nil, fmt.Errorf("agent: get overview campaign: %w", err)
	}
	overview.WeekStart = &weekStart

	const countsQ = `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('sent', 'replied')) AS texted_count,
			COUNT(*) FILTER (WHERE status = 'replied') AS replied_count,
			COUNT(*) FILTER (WHERE status = 'sent') AS waiting_count,
			COUNT(*) FILTER (WHERE status = 'replied' AND parse_status = 'parsed') AS parsed_count
		FROM availability_campaign_recipients
		WHERE campaign_id = $1`

	if err := r.db.QueryRow(ctx, countsQ, campaignID).Scan(
		&overview.TextedCount,
		&overview.RepliedCount,
		&overview.WaitingCount,
		&overview.ParsedCount,
	); err != nil {
		return nil, fmt.Errorf("agent: get overview counts: %w", err)
	}
	return &overview, nil
}

func cleanTemplateSID(templateSID *string) *string {
	if templateSID == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*templateSID)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func scanSettings(row pgx.Row) (*Settings, error) {
	var settings Settings
	if err := row.Scan(
		&settings.CoachID,
		&settings.Enabled,
		&settings.TemplateSID,
		&settings.TemplateStatus,
		&settings.PromptDay,
		&settings.PromptTime,
		&settings.Timezone,
		&settings.RequireCoachConfirmation,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("agent: scan settings: %w", err)
	}
	return &settings, nil
}
