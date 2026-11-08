-- Payment processing fees (buyer bears the CHIP processing charge).
--
-- One row per CHIP payment method per workspace: the merchant's own MDR for
-- that method (fixed sen and/or basis points). Nothing here is a hardcoded
-- CHIP rate — a merchant fills in what their acquirer actually charges them.
--
-- fee_percent_bp is BASIS POINTS (100 = 1%). The CHECK keeps it strictly
-- below 100%: at exactly 100% the gross-up `F - fee(F) >= A` has no solution
-- (the fee grows as fast as the amount), so a 100% "rate" would make every
-- checkout unpriceable instead of being caught at save time.
CREATE TABLE payment_fee_rates (
    workspace_id   UUID    NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    method         TEXT    NOT NULL,
    fee_enabled    BOOLEAN NOT NULL DEFAULT false,
    fee_fixed_sen  BIGINT  NOT NULL DEFAULT 0,
    fee_percent_bp BIGINT  NOT NULL DEFAULT 0,
    updated_by     TEXT    NOT NULL DEFAULT '',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, method),
    CONSTRAINT payment_fee_rates_method_chk
        CHECK (method IN ('fpx', 'card', 'ewallet', 'duitnow_qr')),
    CONSTRAINT payment_fee_rates_fixed_chk
        CHECK (fee_fixed_sen >= 0),
    CONSTRAINT payment_fee_rates_percent_chk
        CHECK (fee_percent_bp >= 0 AND fee_percent_bp < 10000)
);

-- The merchant's acknowledgement that the processing fee is passed on to the
-- buyer and is subject to the acquirer/card-issuer's rules. Required before a
-- fee may be switched on: without it the merchant never saw the disclosure
-- sentence the settings page shows, and no fee may reach a buyer.
CREATE TABLE workspace_fee_acknowledgements (
    workspace_id    UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    acknowledged_by TEXT NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-form override (same shape as the per-form CHIP credential override):
-- '' inherits the workspace's fee settings, 'off' switches the processing fee
-- off for this form even when the workspace has it on.
ALTER TABLE forms ADD COLUMN fee_override TEXT NOT NULL DEFAULT '';
ALTER TABLE forms ADD CONSTRAINT forms_fee_override_chk
    CHECK (fee_override IN ('', 'off'));

-- Sale snapshot. The rate is FROZEN at purchase time: a merchant who changes
-- their MDR later must never rewrite what a past buyer was charged.
-- total_sen = subtotal_sen + shipping_sen - discount_sen + fee_sen.
ALTER TABLE sales ADD COLUMN fee_sen BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sales ADD COLUMN fee_method TEXT NOT NULL DEFAULT '';
ALTER TABLE sales ADD COLUMN fee_fixed_sen BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sales ADD COLUMN fee_percent_bp BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sales ADD CONSTRAINT sales_fee_nonneg_chk
    CHECK (fee_sen >= 0 AND fee_fixed_sen >= 0 AND fee_percent_bp >= 0);
