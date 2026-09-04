-- Issue #307: MCP OAuth tokens are bound to the USER, not to a single
-- account. A connected agent operates across all of the user's spaces
-- (up to the user's role in each), so the per-space account binding on the
-- authorization code and token is dropped (made nullable). The account is
-- still recorded when one is in play, but the token's principal is the user;
-- MCP tools resolve the target space per call.
ALTER TABLE oauth_auth_codes ALTER COLUMN account_id DROP NOT NULL;
ALTER TABLE oauth_tokens    ALTER COLUMN account_id DROP NOT NULL;
