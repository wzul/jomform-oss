-- Roll back 000089: drop the device grant from the stored default and from the
-- rows it was added to.
UPDATE oauth_clients
   SET grant_types = array_remove(grant_types, 'urn:ietf:params:oauth:grant-type:device_code');

ALTER TABLE oauth_clients
    ALTER COLUMN grant_types SET DEFAULT '{authorization_code,refresh_token}';
