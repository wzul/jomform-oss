-- Issue #207: form restrictions (Gravity-Forms-style).
--
-- 1. Schedule: a form can have a start datetime and an end datetime. Before
--    start_at the public form shows a pending message; after end_at it shows
--    an expired message; within the window the form renders normally.
-- 2. Entry limit: a form can cap the number of entries (sales for payment
--    forms, submissions for response forms). When reached, submissions are
--    rejected with a limit-reached message.
--
-- All restriction fields are nullable so an unconfigured form behaves exactly
-- as before (no schedule, no limit). The per-state messages are optional
-- overrides; when empty the renderer falls back to the buyer-facing default
-- for the form's resolved language.

ALTER TABLE forms ADD COLUMN IF NOT EXISTS start_at TIMESTAMPTZ;
ALTER TABLE forms ADD COLUMN IF NOT EXISTS end_at TIMESTAMPTZ;
ALTER TABLE forms ADD COLUMN IF NOT EXISTS entry_limit INT;
ALTER TABLE forms ADD COLUMN IF NOT EXISTS pending_message TEXT;
ALTER TABLE forms ADD COLUMN IF NOT EXISTS expired_message TEXT;
ALTER TABLE forms ADD COLUMN IF NOT EXISTS limit_message TEXT;
