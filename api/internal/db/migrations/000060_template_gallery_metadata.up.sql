-- Issue #594: give the three pre-#174 gallery templates the category and the
-- intro line every other template in the library carries.
--
-- Background. The template picker builds each card from forms.config:
--
--   category    <- config.category
--   description <- the value of the FIRST block whose type is 'text'
--
-- (api/internal/httpapi/rest_templates.go listTemplates; the list_templates
-- MCP tool maps the same two keys.) Migration 000027_template_library seeds the
-- whole library with both, but its guard is a NOT EXISTS on (slug,
-- is_template), so it deliberately skipped the three templates that had already
-- been inserted by hand before the gallery existed — the rows it skipped are
-- exactly the ones that never received either field. Their picker cards render
-- a title with no category and no description while the seeded ones render
-- both. Production ids, read 2026-09-18 (recorded for audit; slug is the
-- stable identity the statements key on):
--
--     8142c8f6-3d99-40d7-a6c3-3e566aac4452  tpl-fnb
--     461bb028-d04d-4cb4-b4b6-65acbe39d51b  tpl-booking
--     4283337b-a0ec-464e-98d8-8dd301632de1  tpl-tuition
--
-- What this migration touches: exactly the three rows in the allowlist below,
-- and within those rows only the field the statement's own WHERE reports
-- missing. Two statements with separate preconditions, so a complete row is
-- never rewritten and a second run is a no-op. On a database where 000027 did
-- create these three rows complete (a fresh install), both statements match
-- nothing — that is the intended, harmless outcome.
--
-- Categories follow the convention that already exists: they are the keys of
-- CATEGORY_ICON in web/src/components/TemplatePicker.tsx (an unknown category
-- renders a generic icon) and the values 000027 uses for these three slugs.
-- "F&B" is only the abbreviation inside the title, not a category.
--
-- The intro is an ordinary text block — the same mechanism as the other 11
-- templates, not a separate description field — prepended to match the seeded
-- library, where the text block is always first. Its block id is 'b0-intro' so
-- the rollback can remove exactly what was added.

-- 1. Category, for the rows that have none.
UPDATE forms f
   SET config = f.config || jsonb_build_object('category', t.category),
       updated_at = now()
  FROM (VALUES
    ('tpl-fnb',     'Food & Beverage'),
    ('tpl-booking', 'Booking'),
    ('tpl-tuition', 'Education')
  ) AS t(slug, category)
 WHERE f.slug = t.slug
   AND f.is_template
   AND f.deleted_at IS NULL
   AND COALESCE(f.config->>'category', '') = '';

-- 2. Intro text block, for the rows that have no non-empty text block yet.
--    Prepended so it is the FIRST text block — what the picker reads as the
--    description, and where the seeded library keeps it.
UPDATE forms f
   SET config = jsonb_set(
                  f.config,
                  '{blocks}',
                  jsonb_build_array(
                    jsonb_build_object(
                      'id', 'b0-intro',
                      'type', 'text',
                      'value', t.intro
                    )
                  ) || COALESCE(f.config->'blocks', '[]'::jsonb)
                ),
       updated_at = now()
  FROM (VALUES
    ('tpl-fnb',     'Order from our menu — pay online when you submit.'),
    ('tpl-booking', 'Book your slot — a deposit secures your appointment.'),
    ('tpl-tuition', 'Register for tuition — pay the registration fee to confirm your seat.')
  ) AS t(slug, intro)
 WHERE f.slug = t.slug
   AND f.is_template
   AND f.deleted_at IS NULL
   AND NOT EXISTS (
     SELECT 1
       FROM jsonb_array_elements(COALESCE(f.config->'blocks', '[]'::jsonb)) b
      WHERE b->>'type' = 'text' AND COALESCE(b->>'value', '') <> ''
   );
