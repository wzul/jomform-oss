-- Issue #197: persistent webhook retry queue.
-- Replaces the in-memory `go func + time.Sleep` retry in dispatch2.go with a
-- durable DB table so retries survive an API restart. A sweep job picks up due
-- retries (next_retry_at <= now()) and re-delivers with exponential backoff.
-- On final failure a retry is marked dead_lettered (visible, not dropped) and
-- can be manually re-enqueued from the delivery log / MCP.
CREATE TABLE IF NOT EXISTS webhook_retries (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id    UUID NOT NULL REFERENCES outbound_webhooks(id) ON DELETE CASCADE,
    event         TEXT NOT NULL,
    payload       TEXT NOT NULL,
    attempt       INT NOT NULL DEFAULT 1,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    dead_lettered BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Sweep query: due, non-dead-lettered retries, oldest first.
CREATE INDEX idx_whr_due ON webhook_retries(next_retry_at) WHERE NOT dead_lettered;
-- Manual-retry / per-webhook lookups.
CREATE INDEX idx_whr_webhook ON webhook_retries(webhook_id);
