DROP TABLE IF EXISTS coach_agent_settings;

DROP INDEX IF EXISTS clients_coach_ai_booking_idx;

ALTER TABLE clients
    DROP COLUMN IF EXISTS ai_booking_enabled;
