-- Revert the products trash (see the up migration).
DROP INDEX IF EXISTS idx_products_account_deleted;
ALTER TABLE products DROP COLUMN IF EXISTS deleted_at;
