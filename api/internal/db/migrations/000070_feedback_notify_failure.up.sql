-- Migration 000070: a feedback thread records when its notification FAILED to
-- leave the building.
--
-- WHY. A feedback notification is best-effort: the reply is stored and the
-- email is handed to the #199 queue. When that hand-off failed the error was
-- discarded, so the admin was told "sent", the filer waited for mail that was
-- never queued, and nothing on any surface said otherwise (the board, the
-- console and the audit log all looked healthy because only the thread was
-- stored). The fix logs the failure AND records it here, so the thread itself —
-- the one place an operator is already looking — carries the fact.
--
-- WHAT IT IS NOT. This adds STATE about a notification attempt; it does not
-- change who may read a thread, what a scope is, or how a thread is written.
-- The columns are nullable, default NULL, and NULL means "the last notification
-- attempt for this thread succeeded (or none was attempted)" — so every
-- pre-existing row reads exactly as it did before.
--
--   notify_failed_at     when the last attempt failed (NULL = no known failure)
--   notify_failed_kind   who was not told: 'filer' (the admin reply alert) or
--                        'admin' (the new-thread alert to the platform team)
--   notify_failed_error  the underlying error text, truncated, for the operator
--
-- Lifecycle: set when a notification attempt fails, cleared when a later
-- attempt for the same thread succeeds. There is deliberately no NOT NULL
-- constraint on kind/error: a row cannot be half-set by a writer that forgets
-- one, because the single writer sets all three in one statement.
ALTER TABLE feedback_threads
    ADD COLUMN IF NOT EXISTS notify_failed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS notify_failed_kind TEXT
        CHECK (notify_failed_kind IS NULL OR notify_failed_kind IN ('filer','admin')),
    ADD COLUMN IF NOT EXISTS notify_failed_error TEXT;

-- The triage list wants "which threads have an undelivered notification" without
-- scanning the whole table: a partial index keeps that read cheap and stays tiny
-- (it only ever holds threads whose notification is currently broken).
CREATE INDEX IF NOT EXISTS idx_feedback_threads_notify_failed
    ON feedback_threads (notify_failed_at DESC)
    WHERE notify_failed_at IS NOT NULL;
