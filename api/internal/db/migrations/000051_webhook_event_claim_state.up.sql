-- Migration 000051: inbound webhook event claim state (issue #517).
--
-- webhook_events dedups inbound CHIP callbacks on dedup_key. Previously the row
-- was inserted BEFORE the business logic ran and processed_at was only stamped
-- on success, so a processing failure left a permanently-claimed row with
-- processed_at NULL: every redelivery was answered "duplicate" and never
-- re-processed, losing a paid sale with no path back.
--
-- The row is now a claim/complete record. status tracks the lifecycle:
--   pending    — claimed, no successful attempt yet (re-claimable)
--   processing — an attempt holds the claim (protected by the claim timeout)
--   processed  — business logic committed (permanent duplicate)
--   failed     — an attempt errored (immediately re-claimable)
-- updated_at is the claim's heartbeat: a 'processing' row older than the claim
-- timeout belongs to an attempt that is gone and may be retaken.
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0;
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Backfill: every existing row has either completed (processed_at set) or was
-- claimed by the old insert-first code path without completing. Only the former
-- is a permanent duplicate; the latter must be claimable again so the retry
-- path can settle it, exactly as the fix intends.
UPDATE webhook_events SET status = 'processed' WHERE processed_at IS NOT NULL AND status <> 'processed';

-- Reclaim index: the sweep and the claim conflict path filter on status plus
-- updated_at for rows that have not been processed.
CREATE INDEX IF NOT EXISTS webhook_events_unprocessed_idx
    ON webhook_events (updated_at)
    WHERE processed_at IS NULL;
