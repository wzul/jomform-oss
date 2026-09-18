-- Rollback for 000060_template_gallery_metadata.
--
-- Removes exactly what the up migration added to the same three rows: the
-- category key and the intro text block. The intro block carries the id the up
-- migration gave it ('b0-intro'), and that id marks "this row was repaired by
-- 000060" — so a row the migration never touched, and every other block in a
-- repaired row, is left alone. A no-op on a database that was never repaired.
--
-- The comparison is COALESCE(b->>'id', '') rather than b->>'id': the blocks
-- these three legacy templates already carried have NO id key at all, and
-- NULL <> 'b0-intro' is NULL rather than true, so a bare comparison would drop
-- every original block instead of keeping it.
UPDATE forms f
   SET config = jsonb_set(
                  f.config - 'category',
                  '{blocks}',
                  COALESCE(
                    (SELECT jsonb_agg(b)
                       FROM jsonb_array_elements(COALESCE(f.config->'blocks', '[]'::jsonb)) b
                      WHERE COALESCE(b->>'id', '') <> 'b0-intro'),
                    '[]'::jsonb
                  )
                ),
       updated_at = now()
 WHERE f.slug IN ('tpl-fnb', 'tpl-booking', 'tpl-tuition')
   AND f.is_template
   AND f.deleted_at IS NULL
   AND EXISTS (
     SELECT 1
       FROM jsonb_array_elements(COALESCE(f.config->'blocks', '[]'::jsonb)) b
      WHERE COALESCE(b->>'id', '') = 'b0-intro'
   );
