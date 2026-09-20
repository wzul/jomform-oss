-- Dashboard UI language (language placement): the dashboard's own language is a
-- PER-USER (per-profile) setting, not a per-browser one. It was previously kept
-- only in the browser's localStorage ("jf_lang"), so the same user signing in
-- from another browser or machine started at English.
--
-- NULL / absent = not set yet: the dashboard then falls back to the value the
-- browser already carries (the legacy localStorage key, migrated on first run)
-- and finally to English. This is deliberately NOT the workspace's
-- workspace_settings.default_lang and NOT the per-form buyer-facing language:
-- the first is about who fills in the form, this one is about who is looking at
-- the dashboard.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS lang TEXT
    CHECK (lang IS NULL OR lang IN ('en','ms','zh','ta','ar'));
