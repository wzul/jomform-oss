-- Migration 000018 down: drop the SES suppression list.
DROP TABLE IF EXISTS suppressed_emails;
