-- Migration 000044 (down): drop the per-domain ACME email column.
ALTER TABLE account_domains DROP COLUMN IF EXISTS tls_email;
