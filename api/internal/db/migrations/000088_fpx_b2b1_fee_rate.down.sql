-- Down for 000088: restore the four-method CHECK.
--
-- A stored 'fpx_b2b1' rate cannot survive the narrower constraint, and silently
-- deleting the merchant's row would lose a rate they set. Deleting it here is the
-- only option the old schema allows, so the down migration removes exactly the
-- rows the previous release could not have held.
DELETE FROM payment_fee_rates WHERE method = 'fpx_b2b1';
ALTER TABLE payment_fee_rates DROP CONSTRAINT payment_fee_rates_method_chk;
ALTER TABLE payment_fee_rates ADD CONSTRAINT payment_fee_rates_method_chk
    CHECK (method IN ('fpx', 'card', 'ewallet', 'duitnow_qr'));
