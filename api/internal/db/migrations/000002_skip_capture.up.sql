-- Migration 000002: skip_capture (delayed capture) support + authorized state
ALTER TABLE sales
    ADD COLUMN skip_capture BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN chip_purchase_id TEXT,
    ADD COLUMN authorized BOOLEAN NOT NULL DEFAULT false;

-- captured_at marks when an authorization was captured (timestamp null = not yet).
ALTER TABLE sales
    ADD COLUMN captured_at TIMESTAMPTZ;

ALTER TABLE sales
    DROP CONSTRAINT IF EXISTS sales_status_check;
ALTER TABLE sales
    ADD CONSTRAINT sales_status_check
    CHECK (status IN ('pending', 'authorized', 'paid', 'confirmed', 'refunded', 'cancelled', 'failed'));