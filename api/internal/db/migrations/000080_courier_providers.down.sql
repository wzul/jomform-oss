-- Rollback of the courier provider layer (000080).
--
-- The two ALTER TABLE columns are dropped, and the connection/state tables are
-- dropped. Nothing else references them: a sale's shipping_detail is plain JSONB
-- and keeps whatever it recorded (a historical sale must stay explainable even
-- after a feature is rolled back — see 000079's note about '{}').
DROP TABLE IF EXISTS shipping_courier_states;
DROP TABLE IF EXISTS shipping_courier_accounts;
ALTER TABLE shipping_zones DROP COLUMN IF EXISTS lat;
ALTER TABLE shipping_zones DROP COLUMN IF EXISTS lng;
