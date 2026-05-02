DROP INDEX IF EXISTS users_calendar_token_idx;
ALTER TABLE users DROP COLUMN IF EXISTS calendar_token;
