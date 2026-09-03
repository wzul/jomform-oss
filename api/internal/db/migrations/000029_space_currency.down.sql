-- Issue #185 rollback: drop the per-space currency columns.
ALTER TABLE space_settings DROP COLUMN IF EXISTS currency;
ALTER TABLE sales DROP COLUMN IF EXISTS currency;
ALTER TABLE products DROP COLUMN IF EXISTS currency;
