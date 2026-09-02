-- Issue #77: slug scoping — a public payment link must be reachable only via
-- its registered space subdomain. Two spaces may now share the same form slug;
-- uniqueness is per-space, not global. The legacy bare-apex /f/{slug} path is
-- removed in code; this migration only relaxes the schema constraint and keeps
-- forms.space_id in sync so the per-space unique index is always correct.

-- 1. Drop the global UNIQUE on forms.slug (the collision that blocked two
--    spaces from claiming the same slug).
ALTER TABLE forms DROP CONSTRAINT IF EXISTS forms_slug_key;

-- 2. Backfill any forms whose space_id is NULL from their account's space
--    (defensive: the 000012 migration already did this, but a form created
--    before its account was linked to a space could still be NULL).
UPDATE forms f
   SET space_id = a.space_id
  FROM accounts a
 WHERE a.id = f.account_id
   AND f.space_id IS NULL
   AND a.space_id IS NOT NULL;

-- 3. Enforce per-space uniqueness on live (non-deleted) forms. A form slug
--    must be unique within its space; the same slug may exist in other spaces.
CREATE UNIQUE INDEX IF NOT EXISTS forms_space_slug_key
    ON forms (space_id, slug)
    WHERE deleted_at IS NULL;

-- 4. Keep forms.space_id in sync with the owning account's space_id on every
--    insert/update, so the per-space unique index can never be bypassed by a
--    stale/NULL space_id. (space_id is denormalised from accounts.space_id.)
CREATE OR REPLACE FUNCTION forms_sync_space_id() RETURNS trigger AS $$
BEGIN
    NEW.space_id := (SELECT a.space_id FROM accounts a WHERE a.id = NEW.account_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_forms_sync_space_id ON forms;
CREATE TRIGGER trg_forms_sync_space_id
    BEFORE INSERT OR UPDATE OF account_id ON forms
    FOR EACH ROW EXECUTE FUNCTION forms_sync_space_id();
