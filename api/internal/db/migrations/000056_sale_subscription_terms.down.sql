-- Revert the subscription terms on the sale (issue #560).
ALTER TABLE sales DROP COLUMN subscription_period;
ALTER TABLE sales DROP COLUMN subscription_amount_sen;
ALTER TABLE sales DROP COLUMN subscription_product_id;
