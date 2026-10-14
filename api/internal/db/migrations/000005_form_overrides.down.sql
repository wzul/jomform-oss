ALTER TABLE forms
    DROP COLUMN IF EXISTS chip_method_whitelist,
    DROP COLUMN IF EXISTS chip_email_fallback,
    DROP COLUMN IF EXISTS chip_secret_key_enc,
    DROP COLUMN IF EXISTS chip_brand_id;