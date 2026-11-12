-- oauth_clients.grant_types must include the RFC 8628 device
-- grant, because that is what this server accepts from every registered client.
--
-- The default was '{authorization_code,refresh_token}' while the AS metadata
-- advertised urn:ietf:params:oauth:grant-type:device_code, so a client inserted
-- without an explicit grant_types described a narrower credential than the one
-- it had. /oauth/device_authorization never consults grant_types (it only looks
-- the client up by id), which is why the device flow worked despite the lie.
--
-- The column is not read by any *Select constant: it is fetched by the dedicated
-- OAuthStore.ClientByID accessor, so the migration/scan-drift guard is unaffected.
ALTER TABLE oauth_clients
    ALTER COLUMN grant_types SET DEFAULT
        '{authorization_code,refresh_token,"urn:ietf:params:oauth:grant-type:device_code"}';

-- Existing rows were registered under the old default and describe the same
-- narrower credential. Every one of them can already use the device grant, so
-- make the stored metadata match the behaviour. Idempotent: only rows missing
-- the grant are touched, and array_append is guarded by the NOT (... = ANY).
UPDATE oauth_clients
   SET grant_types = array_append(grant_types, 'urn:ietf:params:oauth:grant-type:device_code')
 WHERE NOT ('urn:ietf:params:oauth:grant-type:device_code' = ANY(grant_types));
