-- Migration 000052 (down): drop the refunded_sen <= total_sen money invariant
-- constraint on sales (issue #527).
ALTER TABLE sales DROP CONSTRAINT IF EXISTS sales_refunded_nonneg_and_le_total;
