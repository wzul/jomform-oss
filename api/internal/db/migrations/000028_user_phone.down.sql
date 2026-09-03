-- Issue #181 down: drop the optional phone column.
ALTER TABLE users DROP COLUMN IF EXISTS phone;
