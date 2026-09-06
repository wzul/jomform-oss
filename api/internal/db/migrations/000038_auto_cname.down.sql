-- Migration 000038 (down): drop the auto-CNAME status column.
ALTER TABLE account_domains DROP COLUMN IF EXISTS auto_cname;
