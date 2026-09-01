-- Migration 000016: restore the Cloudflare OAuth connect (issue #22).
-- #15 Option B removed too much: the cloudflared tunnel stays dead, but the
-- "Login with Cloudflare" OAuth flow (auto CNAME for custom domains) returns.
-- The column stores a sealed JSON blob {"access":"…","refresh":"…"} so the
-- short-lived access token can be refreshed without re-consent.
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS cf_token_enc TEXT NOT NULL DEFAULT '';