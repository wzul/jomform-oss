-- Migration 000015: remove the dead Cloudflare OAuth/tunnel flow (issue #15, Option B).
-- The auto-setup flow never shipped; custom domains are manual CNAME → Verify.
-- IF EXISTS keeps fresh installs (or skipped migrations) unaffected.
ALTER TABLE accounts DROP COLUMN IF EXISTS cf_token_enc;

ALTER TABLE account_domains DROP COLUMN IF EXISTS cf_token_enc;
ALTER TABLE account_domains DROP COLUMN IF EXISTS cf_account_id;
ALTER TABLE account_domains DROP COLUMN IF EXISTS tunnel_id;