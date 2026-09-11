-- Migration 000049 (down): drop the per-form email override columns (issue #453).
ALTER TABLE form_email_settings DROP COLUMN IF EXISTS receipt_enabled_override;
ALTER TABLE form_email_settings DROP COLUMN IF EXISTS notify_merchant_override;
ALTER TABLE form_email_settings DROP COLUMN IF EXISTS html_override;
