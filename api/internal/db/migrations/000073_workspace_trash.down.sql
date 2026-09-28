-- Down for the workspace trash (000073).
--
-- Reverses both halves: the slug uniqueness goes back to the WHOLE table and
-- the mark column is dropped. It FAILS LOUDLY if the up migration's partial
-- index is still doing real work (two rows sharing a slug where at least one is
-- trashed), rather than silently discarding the re-claimed name — restoring the
-- global constraint is only correct when no live row took a trashed row's slug.
DROP INDEX IF EXISTS workspaces_slug_live_key;
ALTER TABLE workspaces ADD CONSTRAINT spaces_slug_key UNIQUE (slug);
ALTER TABLE workspaces DROP COLUMN IF EXISTS deleted_at;
