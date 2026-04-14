-- Reverse of 000001_initial_schema.up.sql
-- Drop tables in reverse dependency order.

DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS availability_intake_conversations;
DROP TABLE IF EXISTS webhook_events;
DROP TABLE IF EXISTS password_reset_tokens;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS gocardless_mandates;
DROP TABLE IF EXISTS session_credits;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS schedule_runs;
DROP TABLE IF EXISTS client_preferred_windows;
DROP TABLE IF EXISTS trainer_working_hours;
DROP TABLE IF EXISTS client_monthly_assignments;
DROP TABLE IF EXISTS clients;
DROP TABLE IF EXISTS coaches;
DROP TABLE IF EXISTS users;

DROP EXTENSION IF EXISTS "btree_gist";
DROP EXTENSION IF EXISTS "citext";
DROP EXTENSION IF EXISTS "pgcrypto";
