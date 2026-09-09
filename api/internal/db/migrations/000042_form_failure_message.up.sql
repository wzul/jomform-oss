-- Migration 000042: per-form payment-failed message (issue #393).
--
-- Adds a failure_message column so a merchant can control what a buyer sees
-- when a payment fails, instead of the hardcoded "pembayaran gagal" status.
-- The message is HTML-sanitized on render (reusing the confirmation
-- sanitizer) and falls back to a sensible default when empty.
--
-- forms is a *Select-backed table, so the column is added to formSelect and
-- every Scan call site in the same PR (migration scan-drift guard).

ALTER TABLE forms ADD COLUMN IF NOT EXISTS failure_message TEXT NOT NULL DEFAULT '';
