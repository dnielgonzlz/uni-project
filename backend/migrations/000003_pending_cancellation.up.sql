-- Add pending_cancellation status and cancellation tracking columns to sessions.
--
-- The new flow for within-24h cancellations:
--   1. Client requests cancellation  → status = 'pending_cancellation'
--   2. Coach approves (lost)         → status = 'cancelled',  no credit
--   3. Coach waives policy           → status = 'cancelled',  credit issued
--
-- The old flow for outside-24h cancellations is unchanged:
--   session immediately → 'cancelled' with credit.

-- Drop the existing status CHECK so we can extend it.
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_status_check;

-- Re-add the constraint with the new value.
ALTER TABLE sessions ADD CONSTRAINT sessions_status_check
    CHECK (status IN ('proposed', 'confirmed', 'cancelled', 'completed', 'pending_cancellation'));

-- Store the reason and timestamp of the cancellation request.
ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS cancellation_reason        TEXT,
    ADD COLUMN IF NOT EXISTS cancellation_requested_at  TIMESTAMPTZ;

-- Index to let coaches quickly find sessions awaiting their decision.
CREATE INDEX IF NOT EXISTS sessions_pending_cancellation_idx
    ON sessions (coach_id, cancellation_requested_at)
    WHERE status = 'pending_cancellation';
