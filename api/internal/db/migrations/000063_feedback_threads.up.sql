-- Migration 000063: in-app feedback — a system-owned ticket system.
--
-- WHY A NEW TABLE, NOT AN EXTENSION OF `submissions`.
-- `submissions` is a once-write record: (id, form_id, account_id, data,
-- is_spam, spam_reason, created_at). It has no status, no thread and no
-- responder, and every existing submissions reader keys on `account_id` /
-- `form_id` (the /sales merged read, the CSV export, the spam queue, MCP
-- list_form_submissions). Adding conversation columns there would put
-- feedback into all of those reads and into the 30-day spam sweep
-- (SubmissionStore.DeleteSpamOlderThan). Two independent tables keep the
-- blast radius at zero for every existing reader.
--
-- WHY THE ROWS CARRY NO account_id AT ALL.
-- A feedback thread belongs to the PERSON who filed it, not to a customer
-- account: the conversation is between JomForm and a user, so the subject is
-- the user, and the ticket must never surface inside any tenant. Storing
-- account_id (or owning the row from a hidden "system account" the way the
-- template gallery does) would leave an account-shaped join path that a future
-- reader could pick up by mistake. With no account_id column, "this can never
-- appear in a customer's /sales, CSV export or spam queue" is structural
-- rather than a convention every new query has to remember:
--
--   * the spam retention sweep deletes from `submissions` and `sales` only,
--   * the /sales merged read resolves a form id against a customer's forms,
--   * the submissions readers require `account_id = ANY($accounts)`.
--
-- The row therefore cannot be reached by any of them, by construction.
--
-- OWNERSHIP / LIFECYCLE DECISIONS (recorded here because they are the design).
--   * owner = feedback_threads.user_id. Not an account, and not an email
--     snapshot: a user who changes their email keeps their threads, because the
--     thread follows the user id.
--   * user_email is a DISPLAY SNAPSHOT of the address at filing time. The
--     triage list shows who filed it even after the user changes address; the
--     live address is always re-read from `users` when notifying.
--   * ON DELETE CASCADE on user_id: a deleted user's conversations go with
--     them. Retaining a deleted person's complaints would be a privacy
--     liability, and nothing else references a thread. (The unverified-account
--     purge only removes rows with verified = false; an active user's feedback
--     is never swept.)
--
-- STATUS IS PER THREAD, NOT PER MESSAGE.
-- "Is this dealt with?" has one answer per conversation. Per-message status
-- would need a max()/aggregate to answer the triage list's own question, and
-- the user's view has no sensible single message to hang a badge on. One status
-- column on the thread is what both surfaces read.
--
-- NOTE ON THE GUARDS THIS FILE MUST RESPECT:
--   * `feedback_threads` / `feedback_messages` have NO `*Select` constant, so
--     TestMigrationColumnsAppearInSelect does not audit them (selectTables in
--     api/internal/dbstore/migration_scan_drift_test.go lists only
--     accounts/forms/products/sales/workspaces/subscriptions). The store keeps
--     its own select constants and scan sites aligned by hand.
--   * TestMigrationsApplyFromZero runs this on an EMPTY database: there is no
--     back-fill and no dependency on rows existing.
--   * Re-running is impossible; Migrate records the file in schema_migrations.

CREATE TABLE IF NOT EXISTS feedback_threads (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The person who filed it. Deliberately NOT an account: see the header.
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Display snapshot of the filer's address at filing time.
    user_email       CITEXT NOT NULL DEFAULT '',
    -- Optional one-line summary. The UI falls back to the first message.
    subject          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'open'
                     CHECK (status IN ('open','in_progress','resolved','closed')),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Bumped on every message so the triage list can order by "needs
    -- attention" and the user's list by "most recently active".
    last_activity_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The user's own list: newest activity first, scoped to one user id.
CREATE INDEX IF NOT EXISTS idx_feedback_threads_user_activity
    ON feedback_threads (user_id, last_activity_at DESC);
-- The super-admin triage list: filter by status, order by activity.
CREATE INDEX IF NOT EXISTS idx_feedback_threads_status_activity
    ON feedback_threads (status, last_activity_at DESC);

CREATE TABLE IF NOT EXISTS feedback_messages (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id      UUID NOT NULL REFERENCES feedback_threads(id) ON DELETE CASCADE,
    -- Who wrote it. The two-way conversation is the whole point of the table.
    author_kind    TEXT NOT NULL CHECK (author_kind IN ('user','admin')),
    -- The admin's user id (NULL for the filer's own messages, and SET NULL if
    -- an admin account is ever removed so the thread keeps its history).
    author_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    body           TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_feedback_messages_thread
    ON feedback_messages (thread_id, created_at ASC);
