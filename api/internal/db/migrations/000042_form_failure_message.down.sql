-- Migration 000042: revert per-form payment-failed message (issue #393).

ALTER TABLE forms DROP COLUMN IF EXISTS failure_message;
