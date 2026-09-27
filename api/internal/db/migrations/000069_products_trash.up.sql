-- Products get a real trash (deleted_at), so "in the bin" is a DIFFERENT state
-- from "not on sale" (active = false).
--
-- Until now products had no deleted_at at all: DELETE /v1/rest/products/{id}
-- just set active=false, and the dashboard labelled the row "Archived". Two
-- different intentions — "take it off sale for a while" and "I want it gone" —
-- were stored as the same bit, which is why a product the merchant meant to
-- remove still showed up in the admin list as merely archived and could not be
-- distinguished from one they were about to sell again.
--
-- This migration adds the second axis. It mirrors what forms already do
-- (000014_forms_soft_delete): a nullable deleted_at, NULL on every live row.
-- active keeps its own meaning and is untouched.
--
-- The uniqueness story that makes a trash work for forms does NOT apply here:
-- products have no slug and no unique index, so trashing a product frees no
-- name and restoring one cannot collide. No partial index is needed.

ALTER TABLE products ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- The live reads all filter `deleted_at IS NULL`; the trash read filters the
-- opposite. Both are account-scoped, so the index leads with account_id.
CREATE INDEX IF NOT EXISTS idx_products_account_deleted ON products (account_id, deleted_at);
