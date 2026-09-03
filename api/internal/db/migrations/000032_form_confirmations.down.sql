-- Issue #179: revert form confirmations.

ALTER TABLE sales DROP COLUMN IF EXISTS fields;
DROP TABLE IF EXISTS form_confirmations;
