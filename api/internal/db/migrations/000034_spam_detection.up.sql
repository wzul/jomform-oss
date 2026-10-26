-- Issue #245: AI spam detection (background decision-model judge).
--
-- submissions + sales gain is_spam (bool, default false) + spam_reason
-- (nullable text). A submission/sale is accepted immediately, then judged
-- in the background by the AI spam judge; spam entries are flagged here and
-- auto-deleted after 30 days by a retention sweep. Un-spammed entries are
-- never auto-deleted.
--
-- sales has a *Select constant (saleSelect), so the columns are added to
-- saleSelect + scanSale + the Sale struct to keep the scan-drift guard green.
ALTER TABLE submissions
    ADD COLUMN is_spam BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN spam_reason TEXT;

ALTER TABLE sales
    ADD COLUMN is_spam BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN spam_reason TEXT;

CREATE INDEX submissions_spam_created_idx ON submissions (is_spam, created_at);
CREATE INDEX sales_spam_created_idx ON sales (is_spam, created_at);
