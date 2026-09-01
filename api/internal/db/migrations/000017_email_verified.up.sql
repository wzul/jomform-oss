-- Migration 000017: email verification requirement (issue #42).
--
-- New registrations are created with verified=false and may not log in until
-- they confirm their address via the signed /v1/auth/verify link. Existing
-- users are set verified=true in this same migration so NOBODY is locked out
-- at deploy time — the requirement only applies to accounts created going
-- forward.
ALTER TABLE users ADD COLUMN IF NOT EXISTS verified BOOLEAN NOT NULL DEFAULT true;
