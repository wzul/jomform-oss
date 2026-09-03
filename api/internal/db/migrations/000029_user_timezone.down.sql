-- Issue #191 down: drop the optional timezone column.
ALTER TABLE users DROP COLUMN IF EXISTS timezone;
