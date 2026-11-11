-- RFC 8628 device authorization for HEADLESS MCP onboarding (card t_9a6cc85f).
--
-- WHY. Today a person has to open a browser exactly once per MCP client: the
-- authorization-code flow needs the dashboard session to consent. An AI
-- harness (Claude Desktop, Cursor, a headless agent) cannot do that. The
-- device grant replaces the browser consent step with an EMAIL to the
-- account's registered address: the client asks for a device_code, we mail the
-- user_code, and the harness polls the token endpoint until the human approved
-- it in their inbox.
--
-- The email IS the consent screen, so the row records what the person is
-- consenting to: client_id (resolved to its registered name at send time),
-- the requested scope, the requesting IP and user agent (the mail names the
-- city), and when it expires.
--
-- BOTH SECRETS ARE HASHED. `device_code` and `user_code` are bearer secrets:
-- whoever holds either can turn it into a token (once approved). The table
-- therefore stores SHA-256 hashes only, the same rule oauth_auth_codes and
-- oauth_tokens already follow — a database dump must not hand out sessions.
--
-- user_id IS NULLABLE. The authorization request is identified by
-- `login_hint` (RFC 8628 §3.1), an address that may not belong to anybody. The
-- response is deliberately IDENTICAL whether or not it does (any difference
-- would be an account-enumeration oracle), so an unmatched address still gets a
-- device_code row — it simply never receives a mail and can never be approved
-- except by a signed-in person typing the code into the dashboard, which then
-- binds the row to that person.
CREATE TABLE IF NOT EXISTS oauth_device_codes (
    -- sha256 hex of the raw device_code handed to the client. Primary key, so a
    -- poll is a single indexed lookup and a second consumer of the same device
    -- code cannot exist.
    device_hash     TEXT PRIMARY KEY,
    -- sha256 hex of the raw user_code delivered by email. Unique, so the
    -- dashboard approve step (which looks a row up by the code the person was
    -- mailed) can never be ambiguous between two live authorisations.
    user_code_hash  TEXT NOT NULL UNIQUE,
    client_id       TEXT NOT NULL,
    user_id         UUID REFERENCES users(id) ON DELETE CASCADE,
    -- Optional metadata, exactly like oauth_tokens.account_id (issue #307): the
    -- principal is the USER, the account only records which of their
    -- workspaces was in play.
    account_id      UUID REFERENCES accounts(id) ON DELETE SET NULL,
    -- The scope the CLIENT requested, resolved through oauthmeta.GrantedScopes.
    -- Approval may only narrow this set, never widen it.
    scope           TEXT[] NOT NULL DEFAULT '{}',
    ip              TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ NOT NULL,
    -- RFC 8628 §3.2 `interval`, seconds between polls. slow_down (RFC 8628
    -- §3.5) ADDS 5s to this column, so the penalty persists for the rest of the
    -- authorisation instead of being forgotten between requests.
    interval_s      INTEGER NOT NULL DEFAULT 5,
    last_polled_at  TIMESTAMPTZ,
    approved_at     TIMESTAMPTZ,
    denied_at       TIMESTAMPTZ,
    -- Set exactly once, by the atomic consume in the token exchange. Single use
    -- is enforced by that UPDATE ... WHERE consumed_at IS NULL, not by a read
    -- followed by a write.
    consumed_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The per-IP hourly rate limit (the email path must not become a mail bomb)
-- and the best-effort prune of spent rows both scan by time.
CREATE INDEX IF NOT EXISTS idx_oauth_device_codes_created
    ON oauth_device_codes (created_at);
-- The per-account hourly rate limit.
CREATE INDEX IF NOT EXISTS idx_oauth_device_codes_user
    ON oauth_device_codes (user_id, created_at DESC);
