-- Issue #191: optional per-user timezone. NULL/empty = Auto (follow the
-- browser/PC). A user can set a hard-coded IANA timezone or leave it blank,
-- so no NOT NULL constraint.
ALTER TABLE users ADD COLUMN timezone TEXT;
