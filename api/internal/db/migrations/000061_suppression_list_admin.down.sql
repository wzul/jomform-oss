-- Rollback for 000061_suppression_list_admin.
--
-- Drops exactly what the up migration added: the ordering index, the two CHECK
-- constraints, and the note/created_by columns. `email` and its CITEXT primary
-- key are untouched — this migration never changed the table's identity, so the
-- table returns to its 000018 shape.
--
-- The backfill in 000061 is NOT reversed: the pre-000061 free-text reason and
-- the canonical code it was mapped to are both still present (the code in
-- `reason`, the original text in `note`), and `note` is dropped by this
-- rollback. A row that predates 000061 therefore loses only the duplicate
-- copy; the canonical reason survives. Reversing the mapping instead would
-- re-introduce values the CHECK on the next up-migration would have to
-- normalise again, and would silently rewrite rows 000061 never touched.
ALTER TABLE suppressed_emails DROP CONSTRAINT IF EXISTS suppressed_emails_reason_chk;
ALTER TABLE suppressed_emails DROP CONSTRAINT IF EXISTS suppressed_emails_source_chk;
DROP INDEX IF EXISTS idx_suppressed_emails_created_at;
ALTER TABLE suppressed_emails DROP COLUMN IF EXISTS created_by;
ALTER TABLE suppressed_emails DROP COLUMN IF EXISTS note;
