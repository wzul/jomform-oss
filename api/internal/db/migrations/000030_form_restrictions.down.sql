-- Issue #207: revert form restrictions.

ALTER TABLE forms DROP COLUMN IF EXISTS limit_message;
ALTER TABLE forms DROP COLUMN IF EXISTS expired_message;
ALTER TABLE forms DROP COLUMN IF EXISTS pending_message;
ALTER TABLE forms DROP COLUMN IF EXISTS entry_limit;
ALTER TABLE forms DROP COLUMN IF EXISTS end_at;
ALTER TABLE forms DROP COLUMN IF EXISTS start_at;
