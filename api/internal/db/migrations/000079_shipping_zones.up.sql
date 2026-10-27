-- Shipping zones + rates (phase 1 of the shipping feature).
--
-- WHY. A builder `shipping` block has always been a PRICED CHOICE LIST typed by
-- hand inside one form: {"id":"s1","type":"shipping","value":"Postage",
-- "choices":[{"label":"West Malaysia","value":"wm","price_sen":800}]}. Every
-- form repeats the same zone names and prices, nothing is reused across forms,
-- and there is no place to say "free over RM150" or "this rate is for a 5kg
-- parcel". This migration adds the missing rate SOURCE: a workspace-managed set
-- of zones and rate rows that a shipping block can point at, so the fee is
-- resolved from the buyer's address at submit time instead of being retyped.
--
-- The static shape is untouched: a block with no `source` (or source=static)
-- keeps resolving exactly as before from its own `choices`. Nothing here is read
-- by that path.
--
-- PHASE 2 SEAM. The block's `source` also accepts "courier" for the live
-- J&T / PosLaju / Shopee Express / Lalamove quoting that needs merchant accounts
-- Wan must obtain. No courier is called in phase 1 — that source is refused with
-- a clear message rather than silently ignored, and these tables are where a
-- courier's quoted rate will be recorded.
--
-- Money is integer SEN everywhere (price_sen, free_over_sen), never a float.
-- Weight bands are in GRAMS (integer), 0 meaning "no bound".

CREATE TABLE shipping_zone_sets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    -- The set a form's shipping block picks by default (exactly one per account
    -- in practice; nothing enforces that — two defaults is a merchant choice,
    -- and the block names its set explicitly anyway).
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (btrim(name) <> '')
);

CREATE INDEX idx_shipping_zone_sets_account ON shipping_zone_sets(account_id);
CREATE UNIQUE INDEX uq_shipping_zone_sets_account_name
    ON shipping_zone_sets(account_id, lower(btrim(name)));

CREATE TABLE shipping_zones (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    set_id        UUID NOT NULL REFERENCES shipping_zone_sets(id) ON DELETE CASCADE,
    -- Denormalised account scope: every read is scoped by account (or by the
    -- workspace's account set) without a join, the same shape products/forms use.
    account_id    UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    -- Match rules. Any ONE of them matching is enough; the first rule that
    -- matches is recorded on the sale. `states` accepts Malaysian state names
    -- (any spelling in the resolver's alias table) and the region tokens
    -- peninsular / east_malaysia; `districts` matches the buyer's city,
    -- `postcode_from`/`postcode_to` an inclusive numeric postcode range.
    states        TEXT[] NOT NULL DEFAULT '{}',
    districts     TEXT[] NOT NULL DEFAULT '{}',
    postcode_from TEXT,
    postcode_to   TEXT,
    -- Lower priority is evaluated first, so an exception zone (a single
    -- postcode) can be placed above the broad state zone it belongs to.
    priority      INTEGER NOT NULL DEFAULT 0,
    -- The zone that catches an address NOTHING matches, used only when the
    -- block's on_no_match is "default". A fallback zone never matches directly,
    -- so one row serves both "everywhere else" and "we charge a base fee".
    is_fallback   BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (btrim(name) <> '')
);

CREATE INDEX idx_shipping_zones_set ON shipping_zones(set_id);
CREATE INDEX idx_shipping_zones_account ON shipping_zones(account_id);

CREATE TABLE shipping_rates (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    zone_id       UUID NOT NULL REFERENCES shipping_zones(id) ON DELETE CASCADE,
    account_id    UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    -- The buyer-facing name of the rate ("Standard", "Express"), optional.
    label         TEXT NOT NULL DEFAULT '',
    price_sen     BIGINT NOT NULL DEFAULT 0 CHECK (price_sen >= 0),
    -- Weight band in grams; max_weight_g = 0 means "no upper bound".
    min_weight_g  INTEGER NOT NULL DEFAULT 0 CHECK (min_weight_g >= 0),
    max_weight_g  INTEGER NOT NULL DEFAULT 0 CHECK (max_weight_g >= 0),
    -- Free shipping over this ITEMS subtotal (0 = the threshold is disabled).
    -- Crossing it is recorded on the sale as free_over_sen + free=true, never
    -- as a silent 0.
    free_over_sen BIGINT NOT NULL DEFAULT 0 CHECK (free_over_sen >= 0),
    sort          INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_shipping_rates_zone ON shipping_rates(zone_id);
CREATE INDEX idx_shipping_rates_account ON shipping_rates(account_id);

-- What the submit path actually resolved, for the merchant to SEE: the matched
-- zone, the rate row, whether the fee came from the fallback, whether the
-- free-shipping threshold was crossed, and the rule that matched. Before this,
-- shipping was folded into the line items and the only trace was a number.
--
-- '{}' means "no zone-based shipping on this sale" — a static shipping block, a
-- form with no shipping block at all, and every historical sale.
ALTER TABLE sales ADD COLUMN shipping_detail JSONB NOT NULL DEFAULT '{}'::jsonb;
