-- Issue #158: proper space tenancy — every space owns its own account.
--
-- Before this fix, Spaces.Create only inserted a `spaces` row and never
-- linked an account to the new space, so a new space had zero accounts and
-- the dashboard fell back to the user's FIRST account (cross-space data
-- leakage: CHIP keys, domains, webhooks, products, revenue). This migration
-- backfills a fresh, empty account for every space that currently has zero
-- accounts (legacy spaces created before the fix). New spaces created via
-- createSpace now get an account at creation time (code change), so this
-- only covers pre-existing spaces.
--
-- The backfilled account starts with empty CHIP creds, no products, no
-- sales, no domains, no webhooks — a clean slate. CHIP credentials are
-- NEVER shared across spaces.

INSERT INTO accounts (owner_user_id, name, slug, space_id)
SELECT sp.owner_user_id,
       sp.name,
       sp.slug || '-' || substr(replace(sp.id::text, '-', ''), 1, 8),
       sp.id
FROM spaces sp
WHERE NOT EXISTS (SELECT 1 FROM accounts a WHERE a.space_id = sp.id)
ON CONFLICT (slug) DO NOTHING;
