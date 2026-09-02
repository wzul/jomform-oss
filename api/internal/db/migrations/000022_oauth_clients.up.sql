-- Issue #121: OAuth 2.1 authorization code + PKCE flow for MCP clients.
--
-- Three tables replace the old stateless DCR MVP:
--   oauth_clients   — persisted Dynamic Client Registration records so
--                     oauth_register actually records clients and authorize/token
--                     can validate redirect_uri against the registered list
--                     (open-redirect prevention, issue #121 scope 4/5).
--   oauth_auth_codes— short-lived (10-min), single-use authorization codes.
--                     Only the SHA-256 hash is stored (raw code never kept),
--                     bound to client_id + code_challenge (PKCE) + user + account.
--   oauth_tokens    — opaque access + refresh tokens for MCP /mcp auth. Only
--                     SHA-256 hashes stored; bound to account + user; refresh
--                     rotation; revocable (RFC 7009 + account-wide revoke).
--
-- All three are brand-new tables read via dedicated accessor queries (no
-- *Select constant), so the dbstore migration/scan-drift guard is unaffected.

-- 1. Persisted DCR clients.
CREATE TABLE IF NOT EXISTS oauth_clients (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id                 TEXT NOT NULL UNIQUE,      -- e.g. jfc_<hex>
    client_name               TEXT NOT NULL DEFAULT '',
    redirect_uris             TEXT[] NOT NULL DEFAULT '{}',
    token_endpoint_auth_method TEXT NOT NULL DEFAULT 'none',
    grant_types               TEXT[] NOT NULL DEFAULT '{authorization_code,refresh_token}',
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_oauth_clients_client_id ON oauth_clients (client_id);

-- 2. Single-use authorization codes (hashed at rest).
CREATE TABLE IF NOT EXISTS oauth_auth_codes (
    code_hash       TEXT PRIMARY KEY,            -- sha256 of raw code
    client_id       TEXT NOT NULL,
    user_id         UUID NOT NULL,
    account_id      UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    redirect_uri    TEXT NOT NULL,
    code_challenge  TEXT NOT NULL,               -- S256 base64url of the verifier
    scope           TEXT[] NOT NULL DEFAULT '{}',
    expires_at      TIMESTAMPTZ NOT NULL,
    consumed_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_oauth_codes_client ON oauth_auth_codes (client_id);

-- 3. Opaque access + refresh token pair (hashed at rest, revocable).
CREATE TABLE IF NOT EXISTS oauth_tokens (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash         TEXT NOT NULL UNIQUE,        -- sha256 of raw access token
    refresh_hash       TEXT,                        -- sha256 of raw refresh token
    client_id          TEXT NOT NULL,
    user_id            UUID NOT NULL,
    account_id         UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    scope              TEXT[] NOT NULL DEFAULT '{}',
    access_expires_at  TIMESTAMPTZ NOT NULL,
    refresh_expires_at TIMESTAMPTZ,
    revoked_at         TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_refresh ON oauth_tokens (refresh_hash)
    WHERE refresh_hash IS NOT NULL AND revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_account ON oauth_tokens (account_id)
    WHERE revoked_at IS NULL;
