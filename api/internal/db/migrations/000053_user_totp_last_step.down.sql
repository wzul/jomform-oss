-- Migration 000053 (down): drop TOTP replay protection (issue #528).
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_totp_last_step_nonneg;
ALTER TABLE users DROP COLUMN IF EXISTS totp_last_step;
