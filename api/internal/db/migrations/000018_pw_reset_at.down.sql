-- Migration 000018 down: drop the password-reset timestamp column.
ALTER TABLE users DROP COLUMN IF EXISTS pw_reset_at;
