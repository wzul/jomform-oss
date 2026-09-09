-- Migration 000041: media scan reason (issue #364). Rolls back by dropping the
-- column.

ALTER TABLE media DROP COLUMN IF EXISTS scan_reason;
