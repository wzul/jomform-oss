-- Migration 000070 rollback: drop the notification-failure marker.
DROP INDEX IF EXISTS idx_feedback_threads_notify_failed;
ALTER TABLE feedback_threads
    DROP COLUMN IF EXISTS notify_failed_error,
    DROP COLUMN IF EXISTS notify_failed_kind,
    DROP COLUMN IF EXISTS notify_failed_at;
