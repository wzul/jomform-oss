-- Issue #57: per-space default language for buyer-facing form labels.
-- Maintainer decision (2026-09-01): default is English, not BM.
ALTER TABLE space_settings
    ADD COLUMN IF NOT EXISTS default_lang TEXT NOT NULL DEFAULT 'en'
    CHECK (default_lang IN ('en','ms','zh','ta','ar'));
