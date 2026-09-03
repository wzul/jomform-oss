-- Issue #176: form status model — add a password-protected "private" status
-- alongside draft | published (Gravity Forms parity).
--
-- 1. Add a nullable access_password column to forms. It is set only when the
--    form's status is 'private' and holds an argon2id PHC hash of the access
--    password (never the plaintext). Public render shows a password gate for
--    private forms; the correct password unlocks the form.
-- 2. Relax the status CHECK constraint to allow 'private'.

ALTER TABLE forms ADD COLUMN IF NOT EXISTS access_password TEXT;

ALTER TABLE forms DROP CONSTRAINT IF EXISTS forms_status_check;
ALTER TABLE forms ADD CONSTRAINT forms_status_check
    CHECK (status IN ('draft','private','published'));
