-- Revert the variation link on subscriptions (issue #623).
ALTER TABLE subscriptions DROP COLUMN variation_id;
