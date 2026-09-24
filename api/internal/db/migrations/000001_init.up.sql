-- JomForm initial schema (Postgres 18)
-- All money amounts are integer SEN. Timestamps: timestamptz, UTC.

-- Shared extensions live in `public`, named explicitly: an unqualified
-- CREATE EXTENSION creates it in the FIRST schema of the session's search_path
-- (and `IF NOT EXISTS` then matches it by name from anywhere), so a caller
-- running the migrations with `search_path=<other>,public` would strand the
-- extension there and every other caller would fail with
-- `type "citext" does not exist` (SQLSTATE 42704).
CREATE EXTENSION IF NOT EXISTS pgcrypto SCHEMA public;
CREATE EXTENSION IF NOT EXISTS citext SCHEMA public;

-- ─── Identity & tenancy ─────────────────────────────────────────────────────

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         CITEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,              -- argon2id PHC string
    totp_secret   TEXT,                        -- NULL until 2FA setup
    totp_enabled  BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE accounts (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id       UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name                TEXT NOT NULL,
    slug                TEXT NOT NULL UNIQUE,             -- forms live at /f/<slug>
    slug_changed_from   TEXT,                             -- 301 source (renamable, unlike legacy tools)
    custom_domain       TEXT UNIQUE,
    timezone            TEXT NOT NULL DEFAULT 'Asia/Kuala_Lumpur',
    currency            TEXT NOT NULL DEFAULT 'MYR',
    chip_brand_id       TEXT,
    chip_secret_key_enc TEXT,                             -- AES-256-GCM hex (nonce||ct)
    chip_mode           TEXT NOT NULL DEFAULT 'test' CHECK (chip_mode IN ('live','test')),
    bnm_rounding        BOOLEAN NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    key_hash     TEXT NOT NULL UNIQUE,        -- sha256 of the raw key
    name         TEXT NOT NULL,
    scopes       TEXT[] NOT NULL DEFAULT '{readonly}', -- readonly|readwrite|admin
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_api_keys_account ON api_keys(account_id) WHERE revoked_at IS NULL;

-- ─── Catalog ─────────────────────────────────────────────────────────────────

CREATE TABLE products (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id     UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    description    TEXT,
    price_sen      BIGINT NOT NULL CHECK (price_sen >= 0),
    images         JSONB NOT NULL DEFAULT '[]',
    stock          INTEGER CHECK (stock IS NULL OR stock >= 0),  -- NULL = unlimited
    type           TEXT NOT NULL DEFAULT 'one_time' CHECK (type IN ('one_time','subscription')),
    billing_period TEXT CHECK (billing_period IN ('month','year') OR type = 'one_time'),
    trial_days     INTEGER NOT NULL DEFAULT 0 CHECK (trial_days >= 0),
    active         BOOLEAN NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_products_account ON products(account_id);

CREATE TABLE forms (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    slug       TEXT NOT NULL UNIQUE,
    title      TEXT NOT NULL,
    config     JSONB NOT NULL DEFAULT '{}',   -- blocks, fields, payment options
    theme      JSONB NOT NULL DEFAULT '{}',
    status     TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published')),
    is_main    BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_forms_account ON forms(account_id);

-- ─── Sales (one-time) ────────────────────────────────────────────────────────

CREATE TABLE sales (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id        UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    form_id           UUID REFERENCES forms(id) ON DELETE SET NULL,
    ref_code          TEXT NOT NULL UNIQUE,
    client            JSONB NOT NULL,            -- {full_name,email,phone,...}
    items             JSONB NOT NULL,            -- [{product_id,name,price_sen,qty}]
    subtotal_sen      BIGINT NOT NULL DEFAULT 0,
    shipping_sen      BIGINT NOT NULL DEFAULT 0,
    discount_sen      BIGINT NOT NULL DEFAULT 0,
    total_sen         BIGINT NOT NULL DEFAULT 0,
    sale_type         TEXT NOT NULL DEFAULT 'one_time' CHECK (sale_type IN ('one_time','subscription')),
    payment_method    TEXT NOT NULL DEFAULT 'chip' CHECK (payment_method IN ('chip','bank_direct')),
    status            TEXT NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending','paid','confirmed','cancelled','failed')),
    chip_purchase_id  TEXT,
    chip_checkout_url TEXT,
    invoice_no        TEXT UNIQUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sales_account_created ON sales(account_id, created_at DESC);
CREATE INDEX idx_sales_chip_purchase ON sales(chip_purchase_id) WHERE chip_purchase_id IS NOT NULL;

CREATE TABLE bank_direct_proofs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_id    UUID NOT NULL REFERENCES sales(id) ON DELETE CASCADE,
    file_url   TEXT NOT NULL,
    paid_at    TIMESTAMPTZ,
    note       TEXT,
    verified_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─── Subscriptions (CHIP card tokenization) ─────────────────────────────────

CREATE TABLE subscriptions (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id             UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    client_email           TEXT NOT NULL,
    product_id             UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    sale_id                UUID REFERENCES sales(id),   -- first paid sale that created the token
    chip_token_purchase_id TEXT NOT NULL,               -- Purchase with is_recurring_token=true
    status                 TEXT NOT NULL DEFAULT 'active'
                           CHECK (status IN ('active','past_due','cancelled')),
    period                 TEXT NOT NULL CHECK (period IN ('month','year')),
    amount_sen             BIGINT NOT NULL,
    next_charge_at         TIMESTAMPTZ NOT NULL,
    cancelled_at           TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_subscriptions_due ON subscriptions(next_charge_at) WHERE status = 'active';

CREATE TABLE subscription_charges (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id   UUID NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    chip_purchase_id  TEXT UNIQUE,
    amount_sen        BIGINT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending','paid','failed')),
    attempt           INTEGER NOT NULL DEFAULT 1,
    charged_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─── Platform plumbing ───────────────────────────────────────────────────────

CREATE TABLE webhook_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   UUID REFERENCES accounts(id) ON DELETE SET NULL,
    source       TEXT NOT NULL DEFAULT 'chip',
    event_type   TEXT,
    payload      JSONB NOT NULL,
    dedup_key    TEXT NOT NULL UNIQUE,     -- chip_purchase_id + event_type + occurred ts
    processed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE outbound_webhooks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    secret     TEXT NOT NULL,
    events     TEXT[] NOT NULL DEFAULT '{sale.paid}',
    active     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE audit_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID REFERENCES accounts(id) ON DELETE SET NULL,
    actor      TEXT NOT NULL,                 -- 'user:<id>' | 'mcp:<key_name>' | 'system'
    action     TEXT NOT NULL,
    entity     TEXT NOT NULL,
    entity_id  TEXT,
    diff       JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_account_created ON audit_logs(account_id, created_at DESC);