-- Migration 000067: chat write proposals (phase 2 of the in-app assistant).
--
-- WHY A TABLE AND NOT AN IN-MEMORY MAP.
-- The proposal lane's whole guarantee is that a proposal stays EXACTLY what the
-- user was shown until the moment they confirm it, and that it is used at most
-- once. Both are properties of stored state:
--
--   * the arguments the model produced are persisted (canonicalised) and the
--     confirm step executes THOSE, not whatever the client sends back;
--   * consumed_at makes "used twice" a row-level fact, and the consume is a
--     single UPDATE ... WHERE consumed_at IS NULL RETURNING, so two concurrent
--     confirms cannot both win;
--   * expires_at bounds the window in which a card the user has stopped looking
--     at can still be confirmed.
--
-- An in-memory map would reset on every deploy/restart, would not be shared
-- across API replicas (a proposal made on replica A would be unknown on B), and
-- would leave the audit trail implicit. The table is small and swept by expiry.
--
-- THE TOKEN IS STORED HASHED, NEVER RAW.
-- token_hash is sha256(raw token). The raw value exists only in the response to
-- the propose call and in the user's browser; a database read (backup, replica,
-- psql session) therefore cannot replay a proposal. This mirrors api_keys, where
-- only the hash is stored.
--
-- OWNERSHIP. A proposal belongs to the PERSON (user_id) and their account:
--   * user_id is what makes "another caller's token" answer exactly like a token
--     that does not exist (the store scopes every read by it, like
--     feedback_threads), and ON DELETE CASCADE means a deleted person's
--     proposals go with them;
--   * account_id is the workspace context the confirm will run in, recorded so
--     the audit row names the account the write landed on.
--
-- NOTE ON THE GUARDS THIS FILE MUST RESPECT:
--   * chat_write_proposals has NO `*Select` constant, so
--     TestMigrationColumnsAppearInSelect does not audit it (selectTables in
--     api/internal/dbstore/migration_scan_drift_test.go lists only
--     accounts/forms/products/sales/workspaces/subscriptions); the store keeps
--     its own select constant and scan sites aligned by hand.
--   * TestMigrationsApplyFromZero runs this on an EMPTY database: no back-fill,
--     no dependency on existing rows.

CREATE TABLE IF NOT EXISTS chat_write_proposals (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- sha256 hex of the opaque token handed to the browser. UNIQUE so a
    -- collision (or a hand-written row) cannot make two proposals share a token.
    token_hash   TEXT NOT NULL UNIQUE,
    -- The person who will confirm it. Every read is scoped by this column.
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- The workspace context the confirm executes in, for the audit row.
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    -- The write tool the model proposed. Constrained to the shape of a tool
    -- name; which names are actually proposable is enforced in Go
    -- (mcpsrv.ChatToolIsProposable), because that set is a product decision
    -- that will change, and a CHECK constraint would make every change a
    -- migration. The confirm path re-checks it, so a row written before the set
    -- shrank cannot be executed.
    tool         TEXT NOT NULL CHECK (tool ~ '^[a-z][a-z0-9_]{2,63}$'),
    -- The arguments, canonicalised (JSON object with sorted keys) by the store.
    -- JSONB normalises key order and whitespace itself; args_hash is kept
    -- alongside so the "the arguments changed" comparison is an exact string
    -- comparison rather than a JSON-equality question.
    args         JSONB NOT NULL,
    args_hash    TEXT NOT NULL,
    -- The server's own rendering of what the write will do, in the user's
    -- language where the tool knows one. Generated from (tool, args) — never
    -- from model prose, so the card cannot describe an effect the arguments do
    -- not have. The model's own explanation is stored separately and shown as
    -- the assistant's wording, not as the authority.
    title        TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    -- The model's own note ("reason"), shown as a quote. Untrusted text: it is
    -- displayed, never parsed.
    note         TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Short by design: a confirmation is decided while the user is reading the
    -- card in the conversation they just had (5 minutes, see chat.ProposalTTL).
    expires_at   TIMESTAMPTZ NOT NULL,
    -- Set once, atomically, by the confirm (or by an explicit cancel). Non-null
    -- means the token is spent: it cannot be confirmed again.
    consumed_at  TIMESTAMPTZ
);

-- The user's own proposals, newest first (the profile/audit view, and the
-- expiry sweep).
CREATE INDEX IF NOT EXISTS idx_chat_write_proposals_user_created
    ON chat_write_proposals (user_id, created_at DESC);
-- The expiry sweep: find the spent/expired rows to delete.
CREATE INDEX IF NOT EXISTS idx_chat_write_proposals_expires
    ON chat_write_proposals (expires_at);
