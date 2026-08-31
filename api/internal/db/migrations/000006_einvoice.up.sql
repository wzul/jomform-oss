-- Migration 000006: e-Invoice (LHDN) account settings
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS einvoice_tin TEXT,
    ADD COLUMN IF NOT EXISTS einvoice_msic TEXT;