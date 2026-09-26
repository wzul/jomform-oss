-- Temporary 301s left behind by a form-slug rename.
--
-- WHY A SEPARATE TABLE, AND WHY IT IS ONLY A FALLBACK
--   A form's slug was fixed at creation: no code path updated forms.slug, so a
--   merchant could not rename a form's URL and the console's slug field silently
--   dropped the value. Renaming is now supported, which creates a new problem:
--   every URL already handed out (a printed QR stand, a link in a menu's
--   order_form_slug, a shared/printed link) still names the OLD slug.
--
--   Wan's rule for the old URL:
--     "301 sementara. kalau ada form lain yg claim, maka url tu akan ke form
--      yang baru" — a temporary 301 to the form's new slug, and a form that
--      later CLAIMS that slug wins: the old URL then resolves to that live form.
--
--   That rule is why this is a fallback table and not a reservation. The lookup
--   order is: resolve the LIVE form by slug first, and only consult this table
--   when nothing live matches (see FormStore.RedirectedFormByAccountSlug). A row
--   here therefore never blocks a create and never shadows a live form, so no
--   cleanup job is needed for the supersede case; a later form with that slug
--   simply wins by being found first.
--
--   The redirect is TEMPORARY: expires_at bounds it (90 days, written by
--   FormStore.RenameSlug). An expired row is simply not consulted and the old
--   URL 404s like any other unknown slug — the same for everyone.
--
-- A re-rename of the same slug REPLACES the row (ON CONFLICT DO UPDATE), so the
-- claimant always wins and the table cannot accumulate two answers for one URL.

CREATE TABLE IF NOT EXISTS form_slug_redirects (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    old_slug   TEXT NOT NULL,
    form_id    UUID NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

-- One answer per (account, old slug): a second rename onto the same old slug
-- replaces the target rather than adding a second row.
CREATE UNIQUE INDEX IF NOT EXISTS form_slug_redirects_account_slug_key
    ON form_slug_redirects (account_id, old_slug);

-- The redirect lookup also has to be able to drop a form's history when the
-- form itself goes away (the FK cascade covers that); this index serves the
-- "which slugs point at this form" read and keeps the cascade cheap.
CREATE INDEX IF NOT EXISTS idx_form_slug_redirects_form ON form_slug_redirects (form_id);
