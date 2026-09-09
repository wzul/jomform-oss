-- Issue #390 rollback: drop 'unlisted' from the forms.status CHECK constraint.

ALTER TABLE forms DROP CONSTRAINT IF EXISTS forms_status_check;
ALTER TABLE forms ADD CONSTRAINT forms_status_check
    CHECK (status IN ('draft','private','published'));
