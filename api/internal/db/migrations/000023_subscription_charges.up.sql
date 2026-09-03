-- Issue #152: subscription billing count — infinite vs fixed N charges.
-- products.subscription_charges INT (NULL = infinite). A subscription with a
-- fixed count stops after the Nth paid charge and is marked 'completed'.
ALTER TABLE products ADD COLUMN subscription_charges INTEGER
    CHECK (subscription_charges IS NULL OR subscription_charges >= 1);

-- Allow subscriptions to reflect completion after the Nth charge.
ALTER TABLE subscriptions DROP CONSTRAINT subscriptions_status_check;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_status_check
    CHECK (status IN ('active','past_due','cancelled','completed'));
