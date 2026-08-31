ALTER TABLE accounts
    DROP COLUMN IF EXISTS chip_method_whitelist,
    DROP COLUMN IF EXISTS chip_email_fallback;