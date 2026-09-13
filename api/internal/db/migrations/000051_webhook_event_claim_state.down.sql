-- Migration 000051 (down): drop the inbound webhook event claim-state columns
-- (issue #517).
DROP INDEX IF EXISTS webhook_events_unprocessed_idx;
ALTER TABLE webhook_events DROP COLUMN IF EXISTS updated_at;
ALTER TABLE webhook_events DROP COLUMN IF EXISTS attempts;
ALTER TABLE webhook_events DROP COLUMN IF EXISTS status;
