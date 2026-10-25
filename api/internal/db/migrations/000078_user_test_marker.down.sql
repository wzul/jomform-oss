-- Reverse 000078_user_test_marker.
DROP INDEX IF EXISTS idx_users_is_test;
ALTER TABLE users DROP COLUMN IF EXISTS is_test;
