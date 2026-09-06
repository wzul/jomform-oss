-- Issue #325: revert the chip_verified column (no data to preserve).
ALTER TABLE accounts DROP COLUMN IF EXISTS chip_verified;
