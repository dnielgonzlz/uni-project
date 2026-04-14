-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";   -- gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS "citext";     -- case-insensitive email
CREATE EXTENSION IF NOT EXISTS "btree_gist"; -- exclusion constraints for no-double-booking

-- ============================================================
-- users
-- ============================================================
CREATE TABLE users (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email        CITEXT      NOT NULL,
    password_hash TEXT       NOT NULL,
    role         TEXT        NOT NULL CHECK (role IN ('coach', 'client', 'admin')),
    full_name    TEXT        NOT NULL,
    phone_e164   TEXT,
    timezone     TEXT        NOT NULL DEFAULT 'Europe/London',
    is_verified  BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMPTZ
);

CREATE UNIQUE INDEX users_email_active_idx ON users (email) WHERE deleted_at IS NULL;
CREATE INDEX users_role_idx ON users (role);

-- ============================================================
-- coaches  (1:1 with users where role = 'coach')
-- ============================================================
CREATE TABLE coaches (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID        NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    business_name    TEXT,
    stripe_account_id TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at       TIMESTAMPTZ
);

-- ============================================================
-- clients  (1:1 with users where role = 'client')
-- ============================================================
CREATE TABLE clients (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID        NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    coach_id           UUID        NOT NULL REFERENCES coaches (id),
    tenure_started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sessions_per_month INT         NOT NULL CHECK (sessions_per_month BETWEEN 1 AND 20),
    priority_score     INT         NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at         TIMESTAMPTZ
);

CREATE INDEX clients_coach_id_active_idx ON clients (coach_id) WHERE deleted_at IS NULL;

-- ============================================================
-- client_monthly_assignments
-- Trainer sets how many sessions a client gets for a given calendar month.
-- For mid-month onboarding the trainer sets a partial count for that month.
-- ============================================================
CREATE TABLE client_monthly_assignments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id    UUID NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    year         SMALLINT NOT NULL CHECK (year >= 2024),
    month        SMALLINT NOT NULL CHECK (month BETWEEN 1 AND 12),
    session_count INT NOT NULL CHECK (session_count BETWEEN 1 AND 20),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (client_id, year, month)
);

-- ============================================================
-- trainer_working_hours
-- ============================================================
CREATE TABLE trainer_working_hours (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    coach_id     UUID        NOT NULL REFERENCES coaches (id) ON DELETE CASCADE,
    day_of_week  SMALLINT    NOT NULL CHECK (day_of_week BETWEEN 0 AND 6), -- 0=Mon ISO
    start_time   TIME        NOT NULL,
    end_time     TIME        NOT NULL CHECK (end_time > start_time),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (coach_id, day_of_week, start_time)
);

-- ============================================================
-- client_preferred_windows
-- ============================================================
CREATE TABLE client_preferred_windows (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id    UUID        NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    day_of_week  SMALLINT    NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
    start_time   TIME        NOT NULL,
    end_time     TIME        NOT NULL CHECK (end_time > start_time),
    source       TEXT        NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'sms', 'whatsapp')),
    collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX client_preferred_windows_client_idx ON client_preferred_windows (client_id);

-- ============================================================
-- schedule_runs
-- Each time a coach triggers the OR-Tools solver, a run is created.
-- ============================================================
CREATE TABLE schedule_runs (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    coach_id      UUID        NOT NULL REFERENCES coaches (id),
    week_start    DATE        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'pending_confirmation'
                              CHECK (status IN ('pending_confirmation', 'confirmed', 'rejected', 'expired')),
    solver_input  JSONB,
    solver_output JSONB,
    expires_at    TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '48 hours',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX schedule_runs_coach_status_idx ON schedule_runs (coach_id, status);

-- ============================================================
-- sessions
-- ============================================================
CREATE TABLE sessions (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    coach_id         UUID        NOT NULL REFERENCES coaches (id),
    client_id        UUID        NOT NULL REFERENCES clients (id),
    schedule_run_id  UUID        REFERENCES schedule_runs (id),
    starts_at        TIMESTAMPTZ NOT NULL,
    ends_at          TIMESTAMPTZ NOT NULL CHECK (ends_at > starts_at),
    status           TEXT        NOT NULL DEFAULT 'proposed'
                                 CHECK (status IN ('proposed', 'confirmed', 'cancelled', 'completed')),
    notes            TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at       TIMESTAMPTZ,

    -- No double-booking: a coach cannot have two active sessions that overlap in time.
    EXCLUDE USING gist (
        coach_id   WITH =,
        tstzrange(starts_at, ends_at, '[)') WITH &&
    ) WHERE (status IN ('proposed', 'confirmed') AND deleted_at IS NULL),

    -- No double-booking: a client cannot have two active sessions that overlap.
    EXCLUDE USING gist (
        client_id  WITH =,
        tstzrange(starts_at, ends_at, '[)') WITH &&
    ) WHERE (status IN ('proposed', 'confirmed') AND deleted_at IS NULL)
);

CREATE INDEX sessions_coach_time_idx  ON sessions (coach_id, starts_at);
CREATE INDEX sessions_client_time_idx ON sessions (client_id, starts_at);
CREATE INDEX sessions_status_time_idx ON sessions (status, starts_at);
CREATE INDEX sessions_run_idx         ON sessions (schedule_run_id);

-- ============================================================
-- payments  (monthly billing, not per-session)
-- ============================================================
CREATE TABLE payments (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id        UUID        NOT NULL REFERENCES clients (id),
    provider         TEXT        NOT NULL CHECK (provider IN ('stripe', 'gocardless')),
    provider_ref     TEXT,
    amount_pence     INT         NOT NULL CHECK (amount_pence > 0),
    currency         TEXT        NOT NULL DEFAULT 'GBP',
    billing_year     SMALLINT    NOT NULL,
    billing_month    SMALLINT    NOT NULL CHECK (billing_month BETWEEN 1 AND 12),
    status           TEXT        NOT NULL DEFAULT 'pending'
                                 CHECK (status IN ('pending', 'paid', 'failed', 'refunded')),
    idempotency_key  TEXT        NOT NULL UNIQUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX payments_provider_ref_idx ON payments (provider, provider_ref);
CREATE INDEX payments_client_idx        ON payments (client_id);
CREATE INDEX payments_client_month_idx  ON payments (client_id, billing_year, billing_month);

-- ============================================================
-- session_credits
-- Issued when a session is cancelled within the allowed window.
-- The client can use the credit to book a replacement session.
-- ============================================================
CREATE TABLE session_credits (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id         UUID        NOT NULL REFERENCES clients (id),
    reason            TEXT        NOT NULL DEFAULT 'cancelled_with_notice',
    source_session_id UUID        NOT NULL REFERENCES sessions (id),
    used_session_id   UUID        REFERENCES sessions (id),
    expires_at        TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX session_credits_client_active_idx
    ON session_credits (client_id)
    WHERE used_session_id IS NULL;

-- ============================================================
-- gocardless_mandates
-- ============================================================
CREATE TABLE gocardless_mandates (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id   UUID        NOT NULL REFERENCES clients (id),
    mandate_id  TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'pending_submission',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX gocardless_mandates_client_idx ON gocardless_mandates (client_id);

-- ============================================================
-- refresh_tokens
-- ============================================================
CREATE TABLE refresh_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);

-- ============================================================
-- password_reset_tokens
-- ============================================================
CREATE TABLE password_reset_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX password_reset_tokens_hash_idx    ON password_reset_tokens (token_hash);
CREATE INDEX password_reset_tokens_expires_idx ON password_reset_tokens (expires_at);

-- ============================================================
-- webhook_events  (inbound idempotency for Stripe / GoCardless / Twilio)
-- ============================================================
CREATE TABLE webhook_events (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    provider    TEXT        NOT NULL,
    event_id    TEXT        NOT NULL,
    payload     JSONB       NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, event_id)
);

-- ============================================================
-- availability_intake_conversations  (SMS state machine per client)
-- ============================================================
CREATE TABLE availability_intake_conversations (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id   UUID        NOT NULL UNIQUE REFERENCES clients (id),
    state       TEXT        NOT NULL DEFAULT 'idle',
    context     JSONB       NOT NULL DEFAULT '{}',
    started_at  TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- audit_log  (append-only)
-- ============================================================
CREATE TABLE audit_log (
    id          BIGSERIAL   PRIMARY KEY,
    user_id     UUID        REFERENCES users (id),
    action      TEXT        NOT NULL,
    entity_type TEXT        NOT NULL,
    entity_id   UUID,
    detail      JSONB,
    ip_address  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX audit_log_user_idx   ON audit_log (user_id);
CREATE INDEX audit_log_entity_idx ON audit_log (entity_type, entity_id);
