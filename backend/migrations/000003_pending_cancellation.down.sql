-- Revert pending_cancellation changes.

DROP INDEX IF EXISTS sessions_pending_cancellation_idx;

ALTER TABLE sessions
    DROP COLUMN IF EXISTS cancellation_reason,
    DROP COLUMN IF EXISTS cancellation_requested_at;

ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_status_check;

ALTER TABLE sessions ADD CONSTRAINT sessions_status_check
    CHECK (status IN ('proposed', 'confirmed', 'cancelled', 'completed'));
