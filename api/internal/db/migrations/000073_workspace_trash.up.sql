-- Workspace trash: a workspace goes to the bin instead of being destroyed.
--
-- WHY. Until now DELETE /v1/workspaces/{id} ran real DELETEs, deepest-first,
-- over the workspace's whole world (accounts, forms, products, sales, api_keys)
-- and the workspace row itself. The design comment in dbstore/workspace.go
-- explains why the ordering is what it is — the FKs are NO ACTION, so a bare
-- `DELETE FROM workspaces` works on literally nothing — and that ordering is
-- REUSED by the purge path, not discarded. What changes is that the delete is
-- now a MARK (deleted_at), so a mistaken delete is recoverable: every row stays
-- on disk, untouched, until a human purges.
--
-- Wan's decision, verbatim: "Buang guard — workspace penuh boleh masuk bin,
-- restore bila perlu (data kekal 100%)".

-- 1. The mark. Nullable, so every existing row is live by definition and the
--    migration is a no-op for data.
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- 2. Re-scope the slug uniqueness to LIVE rows.
--
--    workspaces.slug was declared `TEXT UNIQUE NOT NULL` in 000012_spaces. A
--    trashed workspace holding its slug forever would mean the user can never
--    reuse the name, which breaks the promise that trash is reversible and
--    cheap. forms solved the same problem in 000019_slug_scoping with a PARTIAL
--    unique index over live rows; this follows that pattern exactly so the two
--    trashable objects behave the same way.
--
--    THE CONSTRAINT NAME IS MEASURED, NOT ASSUMED: it is `spaces_slug_key`
--    (the table was renamed spaces -> workspaces in 000045, which renamed
--    indexes and auto-named constraints but left the UNIQUE constraint's
--    original name in place). Verified against a live database:
--      SELECT conname FROM pg_constraint
--       WHERE conrelid='workspaces'::regclass AND contype='u';
--        -> spaces_slug_key
--    The DROP names it explicitly (rather than trusting a default-derived name)
--    and is IF EXISTS so a database that already re-scoped is not an error.
--    The `workspaces_slug_live_key` index name below is the NEW object's own
--    name and is what the down migration reverses.
ALTER TABLE workspaces DROP CONSTRAINT IF EXISTS spaces_slug_key;
CREATE UNIQUE INDEX IF NOT EXISTS workspaces_slug_live_key
    ON workspaces (slug) WHERE deleted_at IS NULL;
