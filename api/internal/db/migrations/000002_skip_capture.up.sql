-- Migration 000002: skip_capture (delayed capture) support + authorized state
ALTER TABLE sales
    ADD COLUMN IF NOT EXISTS skip_capture BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS authorized BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS captured_at TIMESTAMPTZ;

ALTER TABLE sales
    DROP CONSTRAINT IF EXISTS sales_status_check;
ALTER TABLE sales
    ADD CONSTRAINT sales_status_check
    CHECK (status IN ('pending', 'authorized', 'paid', 'confirmed', 'refunded', 'cancelled', 'failed'));