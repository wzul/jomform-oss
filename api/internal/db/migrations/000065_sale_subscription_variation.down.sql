-- Revert the variation on subscription sales (issue #623).
ALTER TABLE sales DROP COLUMN subscription_variation_id;
