-- Issue #623: a per-variation subscription cycle.
--
-- A subscription product carried ONE cycle (billing_period, trial_days,
-- subscription_charges) for every variation, so a merchant could not sell
-- "Size M — monthly RM30" and "Size L — yearly RM300" from the same product:
-- every variation was billed on the product's cycle.
--
-- NULL MEANS "NOT SET — INHERIT THE PRODUCT'S", NEVER "ZERO". That is the
-- whole rule, and it is what makes this migration purely additive: every
-- existing variation already has NULL in these columns, which is exactly the
-- state "no cycle of its own", so every existing subscription product keeps
-- behaving bit-for-bit as it does today. A migration that turned today's
-- working products into error states was the failure mode being avoided.
--
-- billing_period is the GATE: when it is set the variation owns a cycle; when
-- it is NULL the variation has none and the product's entire cycle applies.
-- The other two columns follow the same NULL-means-inherit rule per field, so
-- "not set" and "explicitly set to the product's value" stay distinguishable —
-- pre-filling the product's cycle into every variation would erase the
-- difference and silently make the product-level setting inert for those rows.
--
-- Money stays integer sen (no money column is added here; the variation's own
-- price_sen already covers the rate).

ALTER TABLE product_variations
    ADD COLUMN billing_period TEXT
        CHECK (billing_period IS NULL OR billing_period IN ('month', 'year')),
    ADD COLUMN trial_days INT
        CHECK (trial_days IS NULL OR trial_days >= 0),
    ADD COLUMN subscription_charges INT
        CHECK (subscription_charges IS NULL OR subscription_charges >= 1);
