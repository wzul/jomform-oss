-- Issue #164 down: drop the per-user default space column.

ALTER TABLE users DROP COLUMN IF EXISTS default_space_id;
