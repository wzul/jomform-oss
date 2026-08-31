ALTER TABLE accounts
    DROP COLUMN IF EXISTS einvoice_msic,
    DROP COLUMN IF EXISTS einvoice_tin;