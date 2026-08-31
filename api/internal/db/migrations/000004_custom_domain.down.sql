ALTER TABLE accounts
    DROP COLUMN IF EXISTS custom_domain_verified_at,
    DROP COLUMN IF EXISTS custom_domain_verified;