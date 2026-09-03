-- Issue #151: product variations data model.
--
-- A product is "simple" when it has zero variations (uses product-level
-- price/stock/images); "variable" when it has >=1 variation. Each variation
-- carries its own name, price, stock, images and sort order.
--
-- This is a brand-new table read via dedicated accessor queries (no *Select
-- constant), so the dbstore migration/scan-drift guard is unaffected.

CREATE TABLE IF NOT EXISTS product_variations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,            -- e.g. "Size M", "Red"
    price_sen   BIGINT NOT NULL CHECK (price_sen >= 0),
    stock       INT,                      -- NULL = unlimited
    images      JSONB NOT NULL DEFAULT '[]',
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_product_variations_product
    ON product_variations (product_id, sort_order);
