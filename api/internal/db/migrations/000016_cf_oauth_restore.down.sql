-- Migration 000016 down: drop the restored Cloudflare OAuth token column.
ALTER TABLE accounts DROP COLUMN IF EXISTS cf_token_enc;