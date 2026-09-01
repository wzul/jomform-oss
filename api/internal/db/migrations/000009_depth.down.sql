DROP TABLE IF EXISTS user_identities;
DROP TABLE IF EXISTS outbound_webhook_deliveries;
ALTER TABLE outbound_webhooks DROP COLUMN IF EXISTS next_retry_at;
ALTER TABLE outbound_webhooks DROP COLUMN IF EXISTS attempts;
ALTER TABLE users DROP COLUMN IF EXISTS avatar_url;