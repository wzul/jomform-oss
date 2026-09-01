CREATE TABLE IF NOT EXISTS passkeys (
    id           BYTEA PRIMARY KEY,          -- credential ID (raw)
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    public_key   BYTEA NOT NULL,
    attestation  TEXT NOT NULL DEFAULT 'none',
    transport    TEXT NOT NULL DEFAULT '',
    flags        INT NOT NULL DEFAULT 0,
    sign_count   BIGINT NOT NULL DEFAULT 0,
    aaguid       TEXT NOT NULL DEFAULT '',
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_passkeys_user ON passkeys(user_id);

CREATE TABLE IF NOT EXISTS passkey_challenges (
    key        TEXT PRIMARY KEY,             -- session-scoped random key
    value      TEXT NOT NULL,                -- base64 challenge data
    kind       TEXT NOT NULL,                -- register | login
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_passkey_challenges_exp ON passkey_challenges(expires_at);
