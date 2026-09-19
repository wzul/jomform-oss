-- Revert the per-variation subscription cycle (issue #623).
ALTER TABLE product_variations DROP COLUMN subscription_charges;
ALTER TABLE product_variations DROP COLUMN trial_days;
ALTER TABLE product_variations DROP COLUMN billing_period;
