-- Migration 000044: per-domain ACME contact email (issue #398).
-- The Let's Encrypt contact email for a custom domain's auto-TLS defaults to
-- the domain owner's account email when the domain is verified, and the
-- merchant can change it per domain. Stored on the domain record so each
-- domain's cert is provisioned/renewed with its own email.
ALTER TABLE account_domains ADD COLUMN tls_email TEXT;
