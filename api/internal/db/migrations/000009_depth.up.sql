-- Migration 000009: webhook delivery log + more events in dispatch flow
CREATE TABLE IF NOT EXISTS outbound_webhook_deliveries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id  UUID NOT NULL REFERENCES outbound_webhooks(id) ON DELETE CASCADE,
    event       TEXT NOT NULL,
    payload     TEXT NOT NULL,
    status_code INT,
    success     BOOLEAN NOT NULL DEFAULT false,
    attempt     INT NOT NULL DEFAULT 1,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_owd_webhook ON outbound_webhook_deliveries(webhook_id, created_at DESC);

ALTER TABLE outbound_webhooks
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0;

-- Login with Google (linkable social identities, provider-generic)
CREATE TABLE IF NOT EXISTS user_identities (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider     TEXT NOT NULL,           -- google | azure_ad | github | ...
    provider_uid TEXT NOT NULL,           -- sub claim
    email        TEXT,
    display_name TEXT,
    picture      TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_uid)
);
CREATE INDEX IF NOT EXISTS idx_user_identities_user ON user_identities(user_id);

ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_url TEXT;