-- Issue #176: revert the private status model.
-- Drop the access_password column and restore the draft|published constraint.

ALTER TABLE forms DROP CONSTRAINT IF EXISTS forms_status_check;
ALTER TABLE forms ADD CONSTRAINT forms_status_check
    CHECK (status IN ('draft','published'));

ALTER TABLE forms DROP COLUMN IF EXISTS access_password;
