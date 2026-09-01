-- Migration 000007: multiple custom domains per account (+ optional CF tunnel creds)
CREATE TABLE IF NOT EXISTS account_domains (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    domain       CITEXT UNIQUE NOT NULL,
    provider     TEXT NOT NULL DEFAULT 'cname',       -- cname | cf_tunnel
    verified     BOOLEAN NOT NULL DEFAULT false,
    verified_at  TIMESTAMPTZ,
    cf_token_enc TEXT,                                  -- AES-GCM sealed (tunnel provider only)
    cf_account_id TEXT,
    tunnel_id    TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_account_domains_account ON account_domains(account_id);
CREATE INDEX IF NOT EXISTS idx_account_domains_domain  ON account_domains(domain);