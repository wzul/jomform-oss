-- Issue #77 down: restore global slug uniqueness and drop the per-space
-- index/trigger. Only safe when no two spaces share a slug (the pre-#77
-- invariant); if duplicates exist this will fail loudly rather than corrupt.
DROP TRIGGER IF EXISTS trg_forms_sync_space_id ON forms;
DROP FUNCTION IF EXISTS forms_sync_space_id();
DROP INDEX IF EXISTS forms_space_slug_key;
ALTER TABLE forms ADD CONSTRAINT forms_slug_key UNIQUE (slug);
