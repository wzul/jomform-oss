-- Issue #177: per-form notification email settings (Gravity-Forms-style).
-- One row per form, mirroring space_email_settings (#107). Empty subject/body
-- means "fall back to the space's email settings"; a customised row (non-empty
-- subject or body) overrides the space defaults for that form's receipts.
CREATE TABLE IF NOT EXISTS form_email_settings (
    form_id               UUID PRIMARY KEY REFERENCES forms(id) ON DELETE CASCADE,
    receipt_subject       TEXT NOT NULL DEFAULT '',
    receipt_body          TEXT NOT NULL DEFAULT '',
    receipt_enabled       BOOLEAN NOT NULL DEFAULT true,
    notify_merchant       BOOLEAN NOT NULL DEFAULT false,
    merchant_notify_email TEXT NOT NULL DEFAULT '',
    html                  BOOLEAN NOT NULL DEFAULT false,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
