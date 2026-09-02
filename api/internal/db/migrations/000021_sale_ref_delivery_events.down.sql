-- Issue #106 down: drop the correlation columns and their indexes.
DROP INDEX IF EXISTS idx_sales_receipt_message_id;
ALTER TABLE sales DROP COLUMN IF EXISTS receipt_message_id;

DROP INDEX IF EXISTS idx_ede_sale_ref_time;
ALTER TABLE email_delivery_events DROP COLUMN IF EXISTS sale_ref;
