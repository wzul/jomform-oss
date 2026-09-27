-- Rollback of the workspace slug aliases.
--
-- Dropping the table removes every former address a rename left behind. The
-- workspaces keep the slugs they were renamed TO, so only the old-host fallback
-- goes away — a former host then 404s, which is the pre-rename behaviour for a
-- slug no workspace serves. No data is lost: the table holds no workspace state,
-- only the mapping from an address to its workspace.
DROP TABLE IF EXISTS workspace_slug_aliases;
