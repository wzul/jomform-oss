-- Rollback for 000087: RFC 8628 device authorisations.
--
-- A plain drop. Nothing references this table (a token minted from a device
-- code is an ordinary oauth_tokens row and survives), and the only data lost is
-- the in-flight authorisations, which have a ten-minute life anyway.
DROP INDEX IF EXISTS idx_oauth_device_codes_user;
DROP INDEX IF EXISTS idx_oauth_device_codes_created;
DROP TABLE IF EXISTS oauth_device_codes;
