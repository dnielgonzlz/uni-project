-- notification_outbox: transactional outbox for post-commit side effects.
-- Workers poll for pending rows, attempt delivery, and mark them done (or failed).
-- Using an outbox guarantees notifications are not lost if the process crashes
-- between the DB commit and the actual send.

CREATE TABLE notification_outbox (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   TEXT        NOT NULL,  -- 'session_confirmed' | 'session_reminder' | 'session_cancelled' | 'payment_failed'
    payload      JSONB       NOT NULL,  -- event-specific data (session_id, client_id, etc.)
    status       TEXT        NOT NULL DEFAULT 'pending'
                             CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    attempts     INT         NOT NULL DEFAULT 0,
    last_error   TEXT,                  -- last delivery error message, if any
    process_after TIMESTAMPTZ NOT NULL DEFAULT NOW(),  -- allows delayed/scheduled delivery
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Worker query: claim a batch of pending rows ordered by process_after.
CREATE INDEX notification_outbox_pending_idx
    ON notification_outbox (process_after ASC)
    WHERE status = 'pending';
