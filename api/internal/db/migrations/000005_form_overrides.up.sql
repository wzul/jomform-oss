-- Migration 000005: per-form CHIP overrides (secret stored encrypted in form config? No —
-- dedicated columns; brand + whitelisted methods + email fallback override.
ALTER TABLE forms
    ADD COLUMN IF NOT EXISTS chip_brand_id TEXT,
    ADD COLUMN IF NOT EXISTS chip_secret_key_enc TEXT,
    ADD COLUMN IF NOT EXISTS chip_email_fallback CITEXT,
    ADD COLUMN IF NOT EXISTS chip_method_whitelist TEXT[];