-- Migration 000004: custom domain verification state
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS custom_domain_verified BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS custom_domain_verified_at TIMESTAMPTZ;