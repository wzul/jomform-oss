-- Issue #545 down: drop the quota counter and its cap constraint.

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_media_used_bytes_within_cap;
ALTER TABLE accounts DROP COLUMN IF EXISTS media_used_bytes;
