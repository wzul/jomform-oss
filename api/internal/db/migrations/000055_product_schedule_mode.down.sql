-- Revert the fixed billing schedule option (round 13c).
ALTER TABLE products DROP COLUMN schedule_mode;
