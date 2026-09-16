-- Revert the recurring-charge failure reason (issue #562).
ALTER TABLE subscription_charges DROP COLUMN failure_reason;
