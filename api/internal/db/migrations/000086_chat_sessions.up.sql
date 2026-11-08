-- Chat history: the conversations a person has with the in-app assistant.
--
-- WHY A TABLE AND NOT THE BROWSER. Until this migration the transcript lived in
-- localStorage and was deleted by the sign-out sweep, so logging out (or a
-- session expiring, or a shared device) destroyed every conversation the person
-- had ever had. A conversation is the user's own work — the questions they asked
-- and the answers they read — and it has to survive the browser, the device and
-- the login. It also has to be reachable from a second device.
--
-- WHAT IS *NOT* STORED: a proposal's token. A confirmation card is a single-use
-- capability with a five-minute life (see 000067_chat_write_proposals), so a
-- transcript keeps the WORDS and never the capability. A restored conversation
-- therefore shows the answer and the tool trail, and offers no buttons that could
-- no longer work.
--
-- SCOPING. A session belongs to one PERSON (user_id) in one WORKSPACE
-- (workspace_id). Every read and write is filtered by both, so a second user
-- cannot read it and the same person's other workspace cannot see it either.
--
-- workspace_id IS NULLABLE, deliberately. `accounts.workspace_id` is nullable:
-- a signup creates an account and NO workspace (the user is asked to create one
-- first), and such a person can still use the assistant. "No workspace selected"
-- is therefore its own scope — the same rule the browser-side transcript used
-- when it gave that state its own key segment — rather than a conversation that
-- cannot be saved at all.
--
-- ARCHIVING. archived_at lets a user put a conversation out of the sidebar
-- without destroying it; a delete is a real delete (the messages cascade), which
-- is what a person expects from a bin icon.

CREATE TABLE IF NOT EXISTS chat_sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The person the conversation belongs to. Every read is scoped by this
    -- column; ON DELETE CASCADE means a deleted account takes its conversations
    -- with it.
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- The workspace the conversation was held in (NULL = the account has no
    -- workspace yet, which is a real state for a new signup). ON DELETE CASCADE
    -- for the same reason as user_id: the content is the workspace's, not the
    -- platform's.
    workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
    -- The sidebar label. Empty means "not named yet" and the API falls back to
    -- the first user message (see dbstore/chat_session.go); a user may rename it.
    title        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Bumped on every appended message and on every rename. The sidebar is
    -- ordered by it (most recently used first), so it is the column the list
    -- index leads with after the scope columns.
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at  TIMESTAMPTZ
);

-- The sidebar query: one person's conversations in one workspace, newest first.
CREATE INDEX IF NOT EXISTS idx_chat_sessions_scope_updated
    ON chat_sessions (user_id, workspace_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS chat_messages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    -- "user" or "assistant". The server writes these, never the client: a
    -- client-supplied "system" or "tool" turn is dropped on the message path
    -- (chat.Turn), and the CHECK keeps a hand-written row from forging one.
    role       TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- What the assistant did to produce an answer: the tool trail, so a restored
    -- conversation can still show what was read. Never a proposal token (see the
    -- header) and never a sentence: ids and machine keys only.
    meta       JSONB NOT NULL DEFAULT '{}'::jsonb
);

-- Reading one conversation in order. `id` breaks ties inside one timestamp so the
-- transcript a restore renders is stable between reads.
CREATE INDEX IF NOT EXISTS idx_chat_messages_session
    ON chat_messages (session_id, created_at, id);
