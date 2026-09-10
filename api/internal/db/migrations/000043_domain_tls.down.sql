-- Migration 000043 (down): drop the per-domain TLS columns.
ALTER TABLE account_domains DROP COLUMN IF EXISTS tls_key_enc;
ALTER TABLE account_domains DROP COLUMN IF EXISTS tls_cert_enc;
ALTER TABLE account_domains DROP COLUMN IF EXISTS tls_error;
ALTER TABLE account_domains DROP COLUMN IF EXISTS tls_status;
ALTER TABLE account_domains DROP COLUMN IF EXISTS tls_mode;
