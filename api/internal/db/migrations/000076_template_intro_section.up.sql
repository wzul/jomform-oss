-- Card t_d46c21e0 — the template library's intro line is PROSE, not a field label.
--
-- THE DEFECT. Every one of the 14 seeded gallery templates opens with an intro
-- SENTENCE stored in a block of type `text`:
--
--   {"id":"b1","type":"text","value":"Order from our menu — pay online when you submit."}
--
-- `text` is not prose. It is the palette's only single-line text INPUT and its
-- value is that field's LABEL: the response renderer has painted it as
-- <input type="text"> since card t_99c3a08b, the payment renderer since
-- t_23289ce3 (which is when the six payment templates started showing the
-- wart), and card t_7fb80479 settled the meaning for the rest of the product
-- (formfields.ValidTypes/collectableTypes, the builder palette and panel, the
-- docs). So each of these rows served
--
--   <label class="lbl">Order from our menu — pay online when you submit.</label>
--   <input type="text" name="b1" data-id="b1">
--
-- — the sentence as a field LABEL above an empty input — and, because `text` is
-- collectable, the block was also part of the form's derived field contract: an
-- extra field in a menu checkout, an answer key no buyer ever fills.
--
-- THE FIX. Prose is `section`/`html` (formfields.proseBlockTypes). This
-- migration retypes the library's intro block to `section`, keeping its id, its
-- value and its position in the row's own block order. Nothing else about a row
-- changes: no other block, no title, no theme, no products snapshot.
--
-- WHY A NEW MIGRATION AND NOT AN EDIT TO 000027. 000027 has already run
-- everywhere, production included, and schema_migrations keys on the FILENAME —
-- an edited seed would fix a fresh database only and leave every stored row as
-- it is, and stored rows exist: `form_templates` is `forms` with
-- is_template=true, owned by the `jomform-templates` account 000027 seeds. Same
-- reasoning as 000060, which repaired three of these rows without touching the
-- seed that created them.
--
-- WHAT IT TOUCHES (a named allowlist, not a class sweep):
--   * the 14 library slugs below, and only while the row is a live template
--     (is_template, not soft-deleted);
--   * within a row, only the INTRO block — id 'b1' (000027's seed) or
--     'b0-intro' (the intro 000060 prepends, which is what the three pre-#174
--     rows carry in production) — and only while it is still a non-empty `text`
--     block;
--   * a row that already carries prose (a non-empty `section`/`html` block) is
--     skipped, which is what makes a second run a no-op.
--
-- A merchant's OWN form is deliberately out of scope. There a `text` block is a
-- legitimate short-answer field, and whether a given block's value is a field
-- label ("Nama anda") or a sentence nobody meant as a field exists only in the
-- merchant's head — no statement can decide it, so none is written here.

-- The intro block is rebuilt in place: the array is re-aggregated in its own
-- order (WITH ORDINALITY) and every element is passed through unchanged except
-- the intro, which takes the merged `{"type":"section"}` (jsonb || keeps the
-- left side's keys and lets the right side win on a collision). `type` is the
-- only key rewritten, so the block's id and value survive byte-for-byte.
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
            WHERE b->>'type' = 'text'
              AND COALESCE(b->>'value', '') <> ''
              AND COALESCE(b->>'id', '') IN ('b1', 'b0-intro')
         )
     AND NOT EXISTS (
           SELECT 1 FROM jsonb_array_elements(l.blocks) b
            WHERE b->>'type' IN ('section', 'html')
              AND COALESCE(b->>'value', '') <> ''
         )
)
UPDATE forms f
   SET config = jsonb_set(
                  f.config,
                  '{blocks}',
                  (SELECT jsonb_agg(
                            CASE WHEN b->>'type' = 'text'
                                      AND COALESCE(b->>'value', '') <> ''
                                      AND COALESCE(b->>'id', '') IN ('b1', 'b0-intro')
                                 THEN b || '{"type": "section"}'::jsonb
                                 ELSE b END
                            ORDER BY ord)
                     FROM jsonb_array_elements(i.blocks) WITH ORDINALITY AS e(b, ord))
                ),
       updated_at = now()
  FROM intro i
 WHERE f.id = i.id;
