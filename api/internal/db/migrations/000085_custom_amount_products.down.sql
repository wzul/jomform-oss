-- Reverse 000085_custom_amount_products.
--
-- A custom-amount product cannot be represented by the old two-value CHECK, so
-- the down migration refuses to pretend: it deletes custom-amount products
-- (they cannot be sold without the type) after moving any sale that recorded
-- the type back to one_time. Sales are history and are kept, not destroyed.
DELETE FROM products WHERE type = 'custom_amount';

UPDATE sales SET sale_type = 'one_time' WHERE sale_type = 'custom_amount';

ALTER TABLE sales DROP CONSTRAINT sales_sale_type_check;
ALTER TABLE sales ADD CONSTRAINT sales_sale_type_check
    CHECK (sale_type IN ('one_time','subscription'));

ALTER TABLE products DROP CONSTRAINT products_amount_limits_chk;
ALTER TABLE products DROP CONSTRAINT products_check;
ALTER TABLE products ADD CONSTRAINT products_check
    CHECK (billing_period IN ('month','year') OR type = 'one_time');
ALTER TABLE products DROP CONSTRAINT products_type_check;
ALTER TABLE products ADD CONSTRAINT products_type_check
    CHECK (type IN ('one_time','subscription'));

ALTER TABLE products DROP COLUMN suggested_price_sen;
ALTER TABLE products DROP COLUMN max_price_sen;
ALTER TABLE products DROP COLUMN min_price_sen;
