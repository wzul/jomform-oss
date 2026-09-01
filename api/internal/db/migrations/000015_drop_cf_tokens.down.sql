-- Migration 000015 down: restore the Cloudflare tunnel token columns
-- (shapes match 000007 for account_domains and 000010 for accounts).
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS cf_token_enc TEXT NOT NULL DEFAULT '';

ALTER TABLE account_domains ADD COLUMN IF NOT EXISTS cf_token_enc TEXT;
ALTER TABLE account_domains ADD COLUMN IF NOT EXISTS cf_account_id TEXT;
ALTER TABLE account_domains ADD COLUMN IF NOT EXISTS tunnel_id TEXT;