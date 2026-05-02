-- Weekly WhatsApp availability campaigns for the MVP AI booking agent.

CREATE TABLE IF NOT EXISTS availability_campaigns (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    coach_id    UUID NOT NULL REFERENCES coaches (id) ON DELETE CASCADE,
    week_start  DATE NOT NULL,
    send_at     TIMESTAMPTZ NOT NULL,
    status      TEXT NOT NULL DEFAULT 'scheduled'
                CHECK (status IN ('scheduled', 'sent', 'failed')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (coach_id, week_start)
);

CREATE TABLE IF NOT EXISTS availability_campaign_recipients (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id     UUID NOT NULL REFERENCES availability_campaigns (id) ON DELETE CASCADE,
    client_id       UUID NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    message_sid     TEXT,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'sent', 'failed', 'replied')),
    sent_at         TIMESTAMPTZ,
    replied_at      TIMESTAMPTZ,
    raw_reply       TEXT,
    parsed_windows  JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (campaign_id, client_id)
);

CREATE INDEX IF NOT EXISTS availability_campaigns_coach_week_idx
    ON availability_campaigns (coach_id, week_start DESC);

CREATE INDEX IF NOT EXISTS availability_campaign_recipients_client_idx
    ON availability_campaign_recipients (client_id, created_at DESC);
