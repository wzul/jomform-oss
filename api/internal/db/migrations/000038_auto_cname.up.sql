-- Migration 000038: track whether a custom domain's CNAME was auto-created
-- via the linked Cloudflare account (issue #322). NULL = not attempted /
-- manual path; 'created' = CF record created; 'failed' = CF attempt failed.
ALTER TABLE account_domains ADD COLUMN auto_cname TEXT;
