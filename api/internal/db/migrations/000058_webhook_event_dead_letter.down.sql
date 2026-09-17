-- Revert the webhook dead-letter state (issue #569).
ALTER TABLE webhook_events DROP COLUMN last_error;
