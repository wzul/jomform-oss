-- Migration 000049: per-form email settings override (issue #453).
-- Mirrors the per-workspace email settings (#107) and the #57 language-override
-- pattern: a form may override the workspace's receipt/notification email so a
-- merchant can send different emails for different forms.
--
-- Resolution order is per-form override -> workspace setting -> built-in
-- default. Text fields (subject/body/merchant email) already treat an empty
-- value as "inherit the workspace setting"; the boolean toggles need an explicit
-- tri-state so "off" can be distinguished from "not overridden", so each gains
-- a nullable *_override column where NULL = use the workspace default.
ALTER TABLE form_email_settings ADD COLUMN IF NOT EXISTS receipt_enabled_override BOOLEAN;
ALTER TABLE form_email_settings ADD COLUMN IF NOT EXISTS notify_merchant_override BOOLEAN;
ALTER TABLE form_email_settings ADD COLUMN IF NOT EXISTS html_override BOOLEAN;

-- Backfill: rows that already carried a custom subject/body were, under the
-- pre-#453 behaviour, treated as a full override that also took the toggles
-- from this row. Preserve that exact behaviour by materialising the overrides.
UPDATE form_email_settings
   SET receipt_enabled_override = receipt_enabled,
       notify_merchant_override = notify_merchant,
       html_override = html
 WHERE (receipt_subject <> '' OR receipt_body <> '')
   AND receipt_enabled_override IS NULL;
