-- Issue #181: optional per-user phone number. NULL/empty = not set. Optional —
-- a user can set it or leave it blank, so no NOT NULL constraint.
ALTER TABLE users ADD COLUMN phone TEXT;
