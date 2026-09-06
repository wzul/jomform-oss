-- Issue #325: persist whether the stored CHIP credentials passed a live
-- CHIP API check at save time. The dashboard previously inferred "verified"
-- from "credentials stored", which falsely showed a ✓ for typo'd/revoked keys.
-- chip_verified is set true only after a real CHIP gateway call succeeds.
ALTER TABLE accounts ADD COLUMN chip_verified BOOLEAN NOT NULL DEFAULT false;
