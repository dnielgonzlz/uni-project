-- Add stripe_customer_id to clients for subscription billing
ALTER TABLE clients ADD COLUMN IF NOT EXISTS stripe_customer_id TEXT;

-- ============================================================
-- subscription_plans
-- ============================================================
CREATE TABLE subscription_plans (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    coach_id          UUID        NOT NULL REFERENCES coaches (id) ON DELETE CASCADE,
    name              TEXT        NOT NULL,
    description       TEXT,
    sessions_included INT         NOT NULL CHECK (sessions_included >= 1),
    amount_pence      INT         NOT NULL CHECK (amount_pence >= 100),
    stripe_product_id TEXT,
    stripe_price_id   TEXT,
    active            BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX subscription_plans_coach_idx ON subscription_plans (coach_id);

-- ============================================================
-- client_subscriptions
-- ============================================================
CREATE TABLE client_subscriptions (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id               UUID        NOT NULL UNIQUE REFERENCES clients (id) ON DELETE CASCADE,
    plan_id                 UUID        NOT NULL REFERENCES subscription_plans (id),
    stripe_subscription_id  TEXT        UNIQUE,
    stripe_customer_id      TEXT,
    status                  TEXT        NOT NULL DEFAULT 'incomplete'
                                        CHECK (status IN ('active', 'past_due', 'cancelled', 'paused', 'incomplete')),
    current_period_start    TIMESTAMPTZ,
    current_period_end      TIMESTAMPTZ,
    sessions_balance        INT         NOT NULL DEFAULT 0 CHECK (sessions_balance >= 0),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX client_subscriptions_client_idx ON client_subscriptions (client_id);

-- ============================================================
-- subscription_plan_changes
-- ============================================================
CREATE TABLE subscription_plan_changes (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id UUID        NOT NULL REFERENCES client_subscriptions (id) ON DELETE CASCADE,
    from_plan_id    UUID        NOT NULL REFERENCES subscription_plans (id),
    to_plan_id      UUID        NOT NULL REFERENCES subscription_plans (id),
    requested_by    UUID        NOT NULL REFERENCES users (id),
    status          TEXT        NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- session_balance_ledger
-- ============================================================
CREATE TABLE session_balance_ledger (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID        NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    subscription_id UUID        REFERENCES client_subscriptions (id),
    delta           INT         NOT NULL,
    reason          TEXT        NOT NULL
                                CHECK (reason IN ('subscription_renewal', 'session_booked', 'session_cancelled', 'plan_change_adjustment', 'manual_adjustment')),
    reference_id    UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX session_balance_ledger_client_time_idx ON session_balance_ledger (client_id, created_at);
