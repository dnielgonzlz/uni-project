DROP TABLE IF EXISTS session_balance_ledger;
DROP TABLE IF EXISTS subscription_plan_changes;
DROP TABLE IF EXISTS client_subscriptions;
DROP TABLE IF EXISTS subscription_plans;
ALTER TABLE clients DROP COLUMN IF EXISTS stripe_customer_id;
