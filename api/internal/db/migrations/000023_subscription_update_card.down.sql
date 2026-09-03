-- Issue #154: drop the subscription update-card columns.
ALTER TABLE subscriptions
    DROP COLUMN IF EXISTS card_update_purchase_id,
    DROP COLUMN IF EXISTS card_update_pending;
