-- Migration 000043: per-domain TLS configuration (issue #397).
-- Each custom domain can use auto-TLS (default, Let's Encrypt via tlsmgr)
-- or a merchant-supplied certificate (bring-your-own-cert).
--
--   tls_mode   'auto' (default) | 'custom'
--   tls_status 'pending' | 'active' | 'error' | 'custom' | '' (not verified)
--   tls_error  human reason when tls_status = 'error'
--   tls_cert_enc / tls_key_enc  AES-GCM sealed PEM (custom mode only)
ALTER TABLE account_domains ADD COLUMN tls_mode TEXT NOT NULL DEFAULT 'auto';
ALTER TABLE account_domains ADD COLUMN tls_status TEXT NOT NULL DEFAULT '';
ALTER TABLE account_domains ADD COLUMN tls_error TEXT;
ALTER TABLE account_domains ADD COLUMN tls_cert_enc TEXT;
ALTER TABLE account_domains ADD COLUMN tls_key_enc TEXT;
