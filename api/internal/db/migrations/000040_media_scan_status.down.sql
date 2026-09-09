-- Issue #361: remove the media scan-status column. Reverse of 000040.
ALTER TABLE media DROP COLUMN IF EXISTS scan_status;
