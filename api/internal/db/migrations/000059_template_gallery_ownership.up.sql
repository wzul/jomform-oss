-- Issue #589: realign the gallery templates that were inserted into a merchant
-- account, so every published template is owned by the system templates account.
--
-- Background. The pre-#174 gallery templates (tpl-fnb, tpl-booking, tpl-tuition)
-- were inserted by hand into whichever account made them, rather than into the
-- system templates account that 000027_template_library introduced. Migration
-- 000027 kept them where they were on purpose — its NOT EXISTS guard is keyed on
-- (slug, is_template), so it skipped those three slugs instead of inserting a
-- second copy.
--
-- Consequently the gallery had two owners: 11 templates in jomform-templates and
-- these 3 in a merchant account. Both template queries are satisfied by such a
-- row (the gallery lists every published template with no account filter; the
-- own-templates list is account-scoped), so the same forms row came back from
-- both and the New-form picker — which merges the two lists — carded it twice.
-- It also wrongly presented these three gallery templates as that merchant's own
-- templates.
--
-- This migration moves OWNERSHIP ONLY. It does not delete, rename, re-slug,
-- change status, change config or change theme of any row: the ids stay exactly
-- as they are, so anything that references them keeps working.
--
--   - forms has no template_id column, and cloning a template copies
--     config/theme at clone time, so existing forms do not reference these rows;
--   - the only remaining references are form_notifications rows (the default
--     merchant notification that cloning provisions), which hold form_id and are
--     unaffected by a change of account_id;
--   - the target account has no workspace, and workspace_id is recomputed from
--     account_id by the forms_sync_workspace_id trigger. These rows therefore
--     stop matching the workspace-scoped published-form queries
--     (ListPublishedByWorkspace / ListPublishedByAccountSlug), which is what the
--     bare storefront listing and the sitemap use; the gallery becomes their only
--     public surface, which is the intent for a gallery template.
--
-- Only rows that are a published template AND sit in a non-system account are
-- touched. A template a merchant created themselves is a draft (create_template
-- forces status='draft'), so their own templates are never taken away.
UPDATE forms
   SET account_id = (
         SELECT id FROM accounts WHERE slug = 'jomform-templates'
       )
 WHERE is_template
   AND status = 'published'
   AND deleted_at IS NULL
   AND account_id <> (SELECT id FROM accounts WHERE slug = 'jomform-templates')
   AND EXISTS (SELECT 1 FROM accounts WHERE slug = 'jomform-templates')
   AND EXISTS (
         SELECT 1 FROM users WHERE email = 'templates@system.jomform'
       );
