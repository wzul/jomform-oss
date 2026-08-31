-- Migration 000003: per-account CHIP overrides (form-level fallback lands later)
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS chip_email_fallback CITEXT,
    ADD COLUMN IF NOT EXISTS chip_method_whitelist TEXT[];