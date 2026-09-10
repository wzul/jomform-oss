-- Issue #444: affiliate marketing — a merchant can create affiliate links
-- (?aff=CODE) for each affiliate; sales driven by an affiliate are attributed
-- to that affiliate and commission is computed on paid sales.
CREATE TABLE IF NOT EXISTS affiliates (
    id            TEXT PRIMARY KEY,
    workspace_id  uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    code          TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL DEFAULT '',
    commission_pct NUMERIC(5,2) NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, code)
);

-- Attribute a sale to an affiliate (the ?aff=CODE the buyer landed with).
ALTER TABLE sales ADD COLUMN affiliate_code TEXT;
