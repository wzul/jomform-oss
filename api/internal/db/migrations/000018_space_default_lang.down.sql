-- Issue #57: space_settings.default_lang rollback.
ALTER TABLE space_settings DROP COLUMN IF EXISTS default_lang;
