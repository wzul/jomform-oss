-- Issue #245: AI spam detection (background gemma4 judge).
ALTER TABLE submissions
    DROP COLUMN IF EXISTS spam_reason,
    DROP COLUMN IF EXISTS is_spam;

ALTER TABLE sales
    DROP COLUMN IF EXISTS spam_reason,
    DROP COLUMN IF EXISTS is_spam;

DROP INDEX IF EXISTS submissions_spam_created_idx;
DROP INDEX IF EXISTS sales_spam_created_idx;
