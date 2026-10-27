-- Courier providers (phase 2 of the shipping feature): the workspace's connection
-- to a real courier, and the point a point-based courier is quoted to.
--
-- WHY. Phase 1 lets a form's shipping block charge a rate from the workspace's own
-- zone table. That is a policy the merchant typed — not what a courier will
-- actually charge. Phase 2 asks the courier, and this migration stores WHAT IS
-- NEEDED TO ASK:
--
--   * shipping_courier_accounts: one row per (workspace, provider). It holds the
--     merchant's PICKUP point (a courier quotes a route, so the lane needs an
--     origin that is not the buyer's address), the provider's own environment as
--     far as it can be told, and — for an OAuth provider — the grant, SEALED with
--     the deployment's encryption key. It never holds a plaintext credential.
--   * shipping_courier_states: the single-use OAuth `state` of a connect attempt.
--     The consent step belongs to a human (the merchant opens the authorize link
--     on their own device); the callback can only trust a state it issued, and a
--     state is consumed exactly once.
--   * shipping_zones.lat / .lng: the zone's own point. Lalamove prices a route
--     between two coordinates and Malaysia is intra-city only, so a zone that is
--     meant to be quoted by a point-based courier needs a point. It is OPTIONAL:
--     a zone without one simply cannot be courier-quoted, and the fee falls back
--     to that zone's rate instead of a made-up coordinate.
--
-- ENVIRONMENT IS NOT A COLUMN OF THE CREDENTIALS. Lalamove's environment comes
-- from its key prefix (pk_test_/pk_prod_); EasyParcel's is decided by which
-- account the merchant authorized, so it is recorded PER CONNECTION as
-- 'sandbox' / 'live' / '' (unknown) and shown to the merchant — a simulated rate
-- must never be mistaken for a real one.
--
-- MONEY IS NOT STORED HERE. A quoted fee is written to the sale
-- (sales.shipping_sen + sales.shipping_detail, which phase 1 added); this table
-- only describes the connection.

CREATE TABLE shipping_courier_accounts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    -- lalamove | easyparcel. The value a shipping block names.
    provider   TEXT NOT NULL,
    -- connected | disconnected | error. A disconnected row is KEPT (its origin
    -- and label are the merchant's configuration); only the grant is dropped.
    status     TEXT NOT NULL DEFAULT 'disconnected',
    -- sandbox | live | '' (unknown). For EasyParcel this is what the merchant
    -- authorized; for Lalamove it is the key prefix.
    environment TEXT NOT NULL DEFAULT '',
    -- A human label for the connected account (an EasyParcel account name, a
    -- Lalamove market). Never a credential and never an id token.
    account_label TEXT NOT NULL DEFAULT '',
    -- The PICKUP address a courier is told to collect from. Postcode/state are
    -- what EasyParcel needs; lat/lng are what Lalamove needs (a route between
    -- two points). Both are merchant configuration, not buyer data.
    origin_postcode TEXT NOT NULL DEFAULT '',
    origin_state    TEXT NOT NULL DEFAULT '',
    origin_country  TEXT NOT NULL DEFAULT 'MY',
    origin_lat      TEXT NOT NULL DEFAULT '',
    origin_lng      TEXT NOT NULL DEFAULT '',
    -- Default service code for a provider that has one (Lalamove: MOTORCYCLE,
    -- CAR, VAN). Empty = the adapter's default.
    service_type   TEXT NOT NULL DEFAULT '',
    -- The OAuth grant, sealed (AES-GCM via internal/cryptoutil). EMPTY for a
    -- provider that authenticates per request (Lalamove's HMAC keys) — that
    -- provider's credential lives in its runtime env file and is never copied
    -- here.
    token_sealed   TEXT NOT NULL DEFAULT '',
    token_expires_at   TIMESTAMPTZ,
    refresh_expires_at TIMESTAMPTZ,
    -- A CLASS, never a vendor payload: 'expired', 'refresh_failed', 'rejected'.
    -- It is what the dashboard shows ("reconnect EasyParcel"), and it is bounded
    -- so nothing a vendor says (or any part of a grant) can land in the row.
    last_error     TEXT NOT NULL DEFAULT '',
    connected_at   TIMESTAMPTZ,
    disconnected_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (btrim(provider) <> ''),
    CHECK (status IN ('connected', 'disconnected', 'error'))
);

-- One connection per provider per workspace: a second connect REPLACES the
-- first (re-authorizing the same account must not leave two live grants).
CREATE UNIQUE INDEX uq_shipping_courier_accounts_provider
    ON shipping_courier_accounts(account_id, provider);
CREATE INDEX idx_shipping_courier_accounts_account
    ON shipping_courier_accounts(account_id);

-- The OAuth handshake. A connect attempt issues a state; the callback consumes
-- it. Single use (used_at) and short lived (the connect surface refuses a state
-- older than its TTL), because the authorize link travels through a browser and
-- an expired link is re-issued rather than replayed.
CREATE TABLE shipping_courier_states (
    state        TEXT PRIMARY KEY,
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    provider     TEXT NOT NULL,
    redirect_uri TEXT NOT NULL DEFAULT '',
    -- The workspace (account) the connect was started for is in account_id; the
    -- user who started it is recorded so the callback can attribute the grant.
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    used_at      TIMESTAMPTZ,
    CHECK (btrim(state) <> '')
);

CREATE INDEX idx_shipping_courier_states_account
    ON shipping_courier_states(account_id, created_at DESC);

-- The zone's own point (decimal degrees, TEXT so an invalid value is a readable
-- refusal rather than a silent 0/0 — which would be a real place in the Atlantic).
ALTER TABLE shipping_zones ADD COLUMN lat TEXT NOT NULL DEFAULT '';
ALTER TABLE shipping_zones ADD COLUMN lng TEXT NOT NULL DEFAULT '';
