-- Issue #154: allow a merchant to update the card on an existing subscription.
--
-- Two new columns on subscriptions:
--   card_update_pending       — set when a charge fails and the merchant has
--                               been asked to re-tokenize with a new card.
--                               While true, the 7-day overdue auto-cancel is
--                               paused (CancelOverdueSubs skips these rows).
--   card_update_purchase_id   — the CHIP token purchase created by the
--                               "update card" flow. When the customer pays
--                               (purchase.paid), the subscription's
--                               chip_token_purchase_id is atomically replaced
--                               with this id and the flag is cleared. The old
--                               token is left in CHIP (Wan rule — never
--                               auto-revoke) but is no longer used for charges.
--
-- Both columns are read via the subSelect constant (added to the struct +
-- scan sites in the same PR), so the migration/scan-drift guard stays green.

ALTER TABLE subscriptions
    ADD COLUMN card_update_pending       BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN card_update_purchase_id   TEXT NOT NULL DEFAULT '';
