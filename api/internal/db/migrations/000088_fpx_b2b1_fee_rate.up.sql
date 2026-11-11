-- FPX B2C and FPX B2B1 are two products with two acquirer rates (card t_1d0198e3).
--
-- The buyer now chooses between "FPX" (personal/B2C) and "FPX B2B1"
-- (corporate/B2B1) on the JomForm page, and each carries its own processing-fee
-- rate. The old CHECK allowed one 'fpx' key, which is exactly what made a
-- different rate for the corporate product impossible to store.
--
-- Nothing is rewritten: an existing 'fpx' row keeps its amount and keeps meaning
-- FPX B2C. A workspace that wants a B2B1 rate stores a NEW row for that key, so
-- no merchant silently starts charging a rate they never set. A workspace that
-- previously charged one FPX rate to both products now charges it to B2C only,
-- and CHIP's own rate (not this table) applies to B2B1 until the merchant sets one
-- there.
ALTER TABLE payment_fee_rates DROP CONSTRAINT payment_fee_rates_method_chk;
ALTER TABLE payment_fee_rates ADD CONSTRAINT payment_fee_rates_method_chk
    CHECK (method IN ('fpx', 'fpx_b2b1', 'card', 'ewallet', 'duitnow_qr'));
