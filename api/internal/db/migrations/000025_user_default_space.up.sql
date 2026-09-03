-- Issue #164: per-user default space. When a user with multiple spaces sets a
-- default, login skips the space-picker and goes straight to that space. NULL
-- (or a space that no longer exists) = no default → the picker is shown.
-- ON DELETE SET NULL so deleting a space never leaves a dangling default.

ALTER TABLE users ADD COLUMN default_space_id UUID REFERENCES spaces(id) ON DELETE SET NULL;
