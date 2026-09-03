-- Issue #152 rollback: drop the billing-count column and the 'completed' status.
ALTER TABLE products DROP COLUMN subscription_charges;

ALTER TABLE subscriptions DROP CONSTRAINT subscriptions_status_check;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_status_check
    CHECK (status IN ('active','past_due','cancelled'));
