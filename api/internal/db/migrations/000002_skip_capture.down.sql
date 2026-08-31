ALTER TABLE sales
    DROP CONSTRAINT IF EXISTS sales_status_check;
ALTER TABLE sales
    ADD CONSTRAINT sales_status_check
    CHECK (status IN ('pending', 'paid', 'confirmed', 'cancelled', 'failed'));
ALTER TABLE sales
    DROP COLUMN IF EXISTS skip_capture,
    DROP COLUMN IF EXISTS chip_purchase_id,
    DROP COLUMN IF EXISTS authorized,
    DROP COLUMN IF EXISTS captured_at;