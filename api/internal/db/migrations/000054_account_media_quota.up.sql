-- Issue #545: enforce the per-workspace storage quota reliably.
--
-- The upload handlers previously read the account's used bytes, compared against
-- the cap, then stored the object — a check-then-act with nothing serialising the
-- read against the write, so concurrent uploads all passed against the same
-- snapshot and collectively exceeded the 1GB cap (measured 1.39x with 5 concurrent
-- 100MB uploads). Variants and PDF previews were stored without being counted at
-- all (~64% of an image upload's real bytes), and the avatar path never checked.
--
-- This adds a counter on the account row so the cap can be enforced by a single
-- conditional UPDATE (the database refuses an over-cap write, and concurrent
-- racers serialise on the row lock), and backfills it from the media rows that are
-- the authoritative record of what was actually stored.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS media_used_bytes BIGINT NOT NULL DEFAULT 0;

-- Backfill from existing media rows (non-deleted only, matching TotalBytes).
UPDATE accounts a
   SET media_used_bytes = COALESCE(sub.total, 0)
  FROM (
        SELECT account_id, SUM(size) AS total
          FROM media
         WHERE deleted_at IS NULL
         GROUP BY account_id
       ) sub
 WHERE sub.account_id = a.id;

-- Never let the backfill leave an account above the cap: the counter is a
-- reservation budget, so an over-cap historical total is clamped to the cap.
UPDATE accounts
   SET media_used_bytes = 1073741824
 WHERE media_used_bytes > 1073741824;

-- Guard the invariant at the database level so no future code path can push an
-- account above the cap.
ALTER TABLE accounts
    ADD CONSTRAINT accounts_media_used_bytes_within_cap
    CHECK (media_used_bytes >= 0 AND media_used_bytes <= 1073741824);
