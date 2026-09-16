-- Record WHY a recurring charge failed (issue #562).
--
-- A failed charge recorded only status='failed' with no cause: the gateway error
-- was discarded inline (`_ = s.Subs.AddCharge(...)`), so neither the merchant nor
-- support could tell an expired card from an invalid recurring token from a
-- gateway outage. The reason is now captured on the charge row so it survives
-- log rotation and can be surfaced in the dashboard.
--
-- Nullable: successful charges and every historical row have none.
ALTER TABLE subscription_charges ADD COLUMN failure_reason TEXT;
