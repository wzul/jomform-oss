-- Issue #444: rollback affiliate marketing.
ALTER TABLE sales DROP COLUMN IF EXISTS affiliate_code;
DROP TABLE IF EXISTS affiliates;
