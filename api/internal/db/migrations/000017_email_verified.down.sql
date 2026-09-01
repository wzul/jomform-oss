-- Migration 000017 down: drop the email-verification column.
ALTER TABLE users DROP COLUMN IF EXISTS verified;
