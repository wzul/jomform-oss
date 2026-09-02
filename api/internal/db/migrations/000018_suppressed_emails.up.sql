-- Migration 000018: SES suppression list (issue #56).
--
-- SNS bounce/complaint feedback is recorded here so no outbound send
-- (verification or receipt) ever targets an address that has hard-bounced or
-- complained. Each row records WHY it was suppressed and from what source so
-- ops can inspect and un-suppress a false positive.
CREATE TABLE IF NOT EXISTS suppressed_emails (
    email      CITEXT NOT NULL PRIMARY KEY,
    reason     TEXT NOT NULL,
    source     TEXT NOT NULL,                 -- 'bounce' | 'complaint' | 'manual'
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
