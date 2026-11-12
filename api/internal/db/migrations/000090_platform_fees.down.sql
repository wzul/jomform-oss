-- Revert the platform-fee model (card t_1a17d22d).
--
-- A row stored against fpx_b2b1 cannot survive the narrower method CHECK, so it
-- is removed first. Losing a B2B1 platform amount on a DOWN migration is the
-- honest outcome: the schema that comes back has no place to keep it, and it
-- must not silently become an fpx amount.
DELETE FROM payment_fee_rates WHERE method = 'fpx_b2b1';

ALTER TABLE sales DROP CONSTRAINT IF EXISTS sales_fee_model_chk;
ALTER TABLE sales DROP CONSTRAINT IF EXISTS sales_platform_fee_nonneg_chk;
ALTER TABLE sales DROP COLUMN IF EXISTS fee_identifier;
ALTER TABLE sales DROP COLUMN IF EXISTS fee_model;
ALTER TABLE sales DROP COLUMN IF EXISTS platform_fee_sen;

ALTER TABLE payment_fee_rates DROP CONSTRAINT IF EXISTS payment_fee_rates_method_chk;
ALTER TABLE payment_fee_rates
    ADD CONSTRAINT payment_fee_rates_method_chk
    CHECK (method IN ('fpx', 'card', 'ewallet', 'duitnow_qr'));

ALTER TABLE payment_fee_rates DROP CONSTRAINT IF EXISTS payment_fee_rates_platform_chk;
ALTER TABLE payment_fee_rates DROP COLUMN IF EXISTS platform_fee_sen;
