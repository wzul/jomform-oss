-- Former workspace slugs, kept so an address already in the wild never dies.
--
-- WHY A TABLE. A workspace's slug is its tenant's PUBLIC host
-- ({slug}.jomform.com): it is printed on QR stands, embedded in iframe
-- snippets, handed to affiliates (?aff=CODE links) and stored in other
-- workspaces' settings. Until now a slug was fixed at creation, so renaming was
-- impossible rather than risky. Renaming is now supported, which creates the
-- problem this table solves: every URL already handed out still names the OLD
-- slug, and a rename without this table turns each of them into a 404 the
-- merchant never sees.
--
-- The rule: the old slug is REMEMBERED and keeps resolving to the same
-- workspace, so the change is additive. A request on a former host is answered
-- with a permanent, METHOD-PRESERVING redirect — HTTP 308, not 301: a 301 lets a
-- user agent re-issue a POST as a GET (RFC 9110 §15.4.2), and the served pages
-- post to their own origin, so a page left open across a rename would submit as a
-- GET and meet a 405 — carrying Cache-Control: no-store so the answer cannot
-- outlive this row on a cache's own heuristic. See
-- PublicHandlers.AliasHostRedirect — rather than serving the tenant twice.
--
-- DIFFERENT FROM HOW A FORM-SLUG RENAME BEHAVES, ON PURPOSE. A form's old URL is
-- a TEMPORARY 301 that a later form claiming that slug wins: two forms in one
-- workspace are peers, and the claimant is found first. A workspace slug is not
-- a peer relationship but a PUBLIC IDENTITY: the row here RESERVES the slug, so
-- no other workspace can take it while the alias exists. A former host therefore
-- always moves to the workspace that used to own it, never to a third party that
-- happened to reuse the name. (The form-slug rename landed on a parallel branch;
-- this table deliberately does NOT share its "claimant wins" semantics.)
--
-- ON DELETE CASCADE is load-bearing: it is what makes the workspace-delete
-- feature correct. Deleting a workspace must take its aliases with it,
-- otherwise a freed address would keep redirecting to a host that no longer
-- exists, and the delete's emptiness guard would leave rows behind.
--
-- A rename writes both halves in ONE transaction (WorkspaceStore.Rename), and a
-- rename back onto the old slug removes the row again — a slug that is live
-- again must not also exist as an alias.

CREATE TABLE IF NOT EXISTS workspace_slug_aliases (
    slug         TEXT PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The redirect read is "which workspace does this host belong to" by slug (the
-- primary key covers it) and the reservation check is the same lookup; this
-- index serves the workspace-side read (a workspace's whole alias history) and
-- keeps the CASCADE cheap.
CREATE INDEX IF NOT EXISTS idx_workspace_slug_aliases_workspace
    ON workspace_slug_aliases (workspace_id);
