-- Rollback for 000076_template_intro_section.
--
-- Puts the 14 library templates' intro block back to `text` — the shape
-- 000027/000060 seeded — so the rollback is exactly inverse to the up migration
-- and nothing else in a row is touched.
--
-- It keys on what the up migration wrote and nothing else: the same 14 slugs,
-- the intro block's id ('b1' from 000027, 'b0-intro' from 000060) and the
-- `section` type it left behind. A block the up migration never converted — a
-- merchant's or a QA row that already carried prose, a section anywhere else in
-- the row — does not match, and a second run of this statement matches nothing
-- (the intro is `text` again), so it is idempotent in both directions.
--
-- `config->'blocks'` is expanded through the same `jsonb_typeof(...) = 'array'`
-- CASE the up migration and dbstore/form.go's formBlocksArray() use, never
-- COALESCE: COALESCE only replaces SQL NULL, so a non-NULL non-array value
-- would be handed straight to jsonb_array_elements and raise SQLSTATE 22023.
WITH library AS (
  SELECT f.id,
         CASE WHEN f.config IS NOT NULL
                   AND jsonb_typeof(f.config) = 'object'
                   AND jsonb_typeof(f.config->'blocks') = 'array'
              THEN f.config->'blocks' ELSE '[]'::jsonb END AS blocks
    FROM forms f
   WHERE f.is_template
     AND f.deleted_at IS NULL
     AND f.slug = ANY (ARRAY[
       'tpl-fnb', 'tpl-booking', 'tpl-tuition', 'tpl-event', 'tpl-donation', 'tpl-ecommerce',
       'tpl-contact-simple', 'tpl-contact-advanced', 'tpl-newsletter', 'tpl-survey',
       'tpl-quote', 'tpl-employment', 'tpl-gift', 'tpl-contest'])
),
intro AS (
  SELECT l.id, l.blocks
    FROM library l
   WHERE EXISTS (
           SELECT 1 FROM jsonb_array_elements(l.blocks) b
            WHERE b->>'type' = 'section'
              AND COALESCE(b->>'value', '') <> ''
              AND COALESCE(b->>'id', '') IN ('b1', 'b0-intro')
         )
)
UPDATE forms f
   SET config = jsonb_set(
                  f.config,
                  '{blocks}',
                  (SELECT jsonb_agg(
                            CASE WHEN b->>'type' = 'section'
                                      AND COALESCE(b->>'value', '') <> ''
                                      AND COALESCE(b->>'id', '') IN ('b1', 'b0-intro')
                                 THEN b || '{"type": "text"}'::jsonb
                                 ELSE b END
                            ORDER BY ord)
                     FROM jsonb_array_elements(i.blocks) WITH ORDINALITY AS e(b, ord))
                ),
       updated_at = now()
  FROM intro i
 WHERE f.id = i.id;
