-- MVP AI booking agent settings and per-client allowlist.

ALTER TABLE clients
    ADD COLUMN IF NOT EXISTS ai_booking_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS clients_coach_ai_booking_idx
    ON clients (coach_id, ai_booking_enabled)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS coach_agent_settings (
    coach_id                    UUID PRIMARY KEY REFERENCES coaches (id) ON DELETE CASCADE,
    enabled                     BOOLEAN NOT NULL DEFAULT FALSE,
    template_sid                TEXT,
    template_status             TEXT NOT NULL DEFAULT 'missing'
                                CHECK (template_status IN ('missing', 'pending', 'approved', 'rejected')),
    prompt_day                  TEXT NOT NULL DEFAULT 'friday',
    prompt_time                 TIME NOT NULL DEFAULT '18:00',
    timezone                    TEXT NOT NULL DEFAULT 'Europe/London',
    require_coach_confirmation  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
