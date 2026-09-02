-- Issue #92: event tracking (audit trail + behaviour analytics) + SES email
-- delivery events. Postgres now, ClickHouse-ready schema: both tables are
-- append-only and immutable (no UPDATE/DELETE except the retention sweep), use
-- BIGSERIAL + created_at as the natural partition key, and carry only the two
-- time indexes + JSONB that current scale needs. A future ClickHouse export is
-- a straight COPY/stream into a MergeTree partitioned by toYYYYMM(created_at).

-- 1. events — generic user-action audit/behaviour log. Every row is a
--    meaningful state-changing action (auth, forms, sales, settings, uploads),
--    never a keystroke. account_id is NOT NULL (every recorded action belongs
--    to an account); user_id is nullable (system/API-key actions have no user).
CREATE TABLE events (
    id          BIGSERIAL PRIMARY KEY,
    account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    user_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    event_name  TEXT NOT NULL,                 -- e.g. form.created, sale.confirmed, login
    properties  JSONB NOT NULL DEFAULT '{}',   -- event-specific payload (form_id, ref_code, ...)
    ip          INET,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_account_time ON events (account_id, created_at DESC);
CREATE INDEX idx_events_name_time   ON events (event_name, created_at DESC);
CREATE INDEX idx_events_user_time   ON events (user_id, created_at DESC);

-- 2. email_delivery_events — SES delivery/bounce/complaint feedback. account_id
--    is nullable for system mail (no owning account). Rows are append-only.
CREATE TABLE email_delivery_events (
    id          BIGSERIAL PRIMARY KEY,
    account_id  UUID REFERENCES accounts(id) ON DELETE CASCADE,  -- nullable for system mail
    email       TEXT NOT NULL,
    event_type  TEXT NOT NULL,   -- delivered | bounce | complaint | open | click
    bounce_type TEXT,            -- Permanent | Transient (when bounce)
    detail      JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ede_email_time ON email_delivery_events (email, created_at DESC);
CREATE INDEX idx_ede_type_time   ON email_delivery_events (event_type, created_at DESC);
