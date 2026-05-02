-- Add a long-lived, opaque calendar token to each user.
-- This token is embedded in the ICS subscription URL and never expires,
-- so calendar apps can keep syncing without needing a JWT refresh.
-- The user can regenerate it from their profile settings to revoke access.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS calendar_token UUID NOT NULL DEFAULT gen_random_uuid();

CREATE UNIQUE INDEX IF NOT EXISTS users_calendar_token_idx ON users (calendar_token);
